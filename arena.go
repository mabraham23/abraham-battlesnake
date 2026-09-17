package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BattlesnakeOfficial/rules"
	"github.com/BattlesnakeOfficial/rules/maps"
)

type ArenaSettings struct {
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Ruleset         string `json:"ruleset"`
	Map             string `json:"map"`
	TimeoutMS       int    `json:"timeout_ms"`
	MaxTurns        int    `json:"max_turns"`
	FoodSpawnChance int    `json:"food_spawn_chance"`
	MinimumFood     int    `json:"minimum_food"`
}
type ArenaJob struct {
	ID     string   `json:"id"`
	Seed   int64    `json:"seed"`
	Snakes []Policy `json:"snakes"`
	Focus  int      `json:"focus"`
	Trace  bool     `json:"trace,omitempty"`
}
type ArenaBatch struct {
	Settings        ArenaSettings `json:"settings"`
	Workers         int           `json:"workers"`
	Games           []ArenaJob    `json:"games"`
	ConcurrentMoves bool          `json:"concurrent_moves,omitempty"`
}
type SnakeMetrics struct {
	Name           string  `json:"name"`
	Moves          int     `json:"moves"`
	Timeouts       int     `json:"timeouts"`
	Errors         int     `json:"errors"`
	MaxMS          float64 `json:"max_ms"`
	P95MS          float64 `json:"p95_ms"`
	Length         int     `json:"length"`
	Eliminated     string  `json:"eliminated"`
	EliminatedTurn int     `json:"eliminated_turn"`
}
type ArenaDecision struct {
	Turn         int     `json:"turn"`
	Seat         int     `json:"seat"`
	ElapsedMS    float64 `json:"elapsed_ms"`
	ResponseMove string  `json:"response_move"`
	Move         string  `json:"move"`
	Timeout      bool    `json:"timeout"`
	Error        string  `json:"error,omitempty"`
}
type ArenaResult struct {
	ID           string          `json:"id"`
	Seed         int64           `json:"seed"`
	Winner       int             `json:"winner"`
	Turns        int             `json:"turns"`
	Truncated    bool            `json:"truncated"`
	Error        string          `json:"error,omitempty"`
	Snakes       []SnakeMetrics  `json:"snakes"`
	Frames       []GameState     `json:"frames,omitempty"`
	Decisions    []ArenaDecision `json:"decisions,omitempty"`
	LastDecision *GameState      `json:"last_decision,omitempty"`
	LastMove     string          `json:"last_move,omitempty"`
}

func (s ArenaSettings) validate() error {
	if s.Ruleset != "standard" && s.Ruleset != "solo" {
		return errors.New("current policies support standard or solo rules only")
	}
	if s.Map != "standard" {
		return errors.New("current policies support the standard map only")
	}
	if s.Width < 7 || s.Height < 7 || s.Width > 25 || s.Height > 25 || s.Width%2 == 0 || s.Height%2 == 0 {
		return errors.New("board dimensions must be odd and between 7 and 25")
	}
	if s.TimeoutMS < 10 || s.TimeoutMS > 5000 || s.MaxTurns < 1 || s.MaxTurns > 2000 || s.FoodSpawnChance < 0 || s.FoodSpawnChance > 100 || s.MinimumFood < 0 || s.MinimumFood > 10 {
		return errors.New("invalid timeout, turn cap or food settings")
	}
	return nil
}

func runArena(ctx context.Context, input io.Reader, output io.Writer) error {
	var batch ArenaBatch
	decoder := json.NewDecoder(io.LimitReader(input, 32<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		return err
	}
	if err := batch.Settings.validate(); err != nil {
		return err
	}
	if batch.Workers < 1 || batch.Workers > runtime.NumCPU() || len(batch.Games) == 0 || len(batch.Games) > 10000 {
		return errors.New("invalid workers or batch size")
	}
	ids := make(map[string]bool)
	for _, job := range batch.Games {
		if job.ID == "" || ids[job.ID] {
			return errors.New("game IDs must be unique and nonempty")
		}
		ids[job.ID] = true
	}
	runtime.GOMAXPROCS(batch.Workers)
	jobs := make(chan ArenaJob)
	results := make(chan ArenaResult)
	var wg sync.WaitGroup
	for range batch.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				results <- simulateGame(ctx, batch.Settings, job, batch.ConcurrentMoves)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, job := range batch.Games {
			select {
			case jobs <- job:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	encoder := json.NewEncoder(output)
	for result := range results {
		if err := encoder.Encode(result); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func simulateGame(ctx context.Context, settings ArenaSettings, job ArenaJob, concurrentMoves bool) (result ArenaResult) {
	result = ArenaResult{ID: job.ID, Seed: job.Seed, Winner: -1}
	defer func() {
		if r := recover(); r != nil {
			result.Error = fmt.Sprintf("simulation panic: %v", r)
		}
	}()
	if err := settings.validate(); err != nil {
		result.Error = err.Error()
		return
	}
	if job.ID == "" || job.Seed == 0 || len(job.Snakes) < 1 || len(job.Snakes) > 4 || job.Focus < 0 || job.Focus >= len(job.Snakes) {
		result.Error = "invalid game ID, nonzero seed, focus or snake count"
		return
	}
	if (settings.Ruleset == "solo") != (len(job.Snakes) == 1) {
		result.Error = "solo requires one snake; standard requires two to four"
		return
	}
	ids := make([]string, len(job.Snakes))
	result.Snakes = make([]SnakeMetrics, len(ids))
	latencies := make([][]float64, len(ids))
	clients := make([]*http.Client, len(ids))
	for i, p := range job.Snakes {
		if err := validatePolicy(p); err != nil {
			result.Error = err.Error()
			return
		}
		ids[i] = fmt.Sprintf("seat-%d", i)
		result.Snakes[i].Name = p.Name
		if p.URL != "" {
			if err := validateLocalURL(p.URL); err != nil {
				result.Error = err.Error()
				return
			}
			clients[i] = &http.Client{Timeout: time.Duration(settings.TimeoutMS) * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}
		}
	}
	params := map[string]string{rules.ParamFoodSpawnChance: strconv.Itoa(settings.FoodSpawnChance), rules.ParamMinimumFood: strconv.Itoa(settings.MinimumFood), rules.ParamHazardDamagePerTurn: "14"}
	engine := rules.NewRulesetBuilder().WithSeed(job.Seed).WithParams(params).NamedRuleset(settings.Ruleset)
	gameMap, err := maps.GetMap(settings.Map)
	if err != nil {
		result.Error = err.Error()
		return
	}
	board, err := maps.SetupBoard(settings.Map, engine.Settings(), settings.Width, settings.Height, ids)
	if err != nil {
		result.Error = err.Error()
		return
	}
	_, board, err = engine.Execute(board, nil)
	if err != nil {
		result.Error = err.Error()
		return
	}
	for i, p := range job.Snakes {
		if clients[i] != nil {
			if _, err := callSnake(ctx, clients[i], p.URL, "start", arenaState(board, settings, job, i)); err != nil {
				result.Snakes[i].Errors++
			}
		}
	}
	previous := make([]string, len(ids))
	for board.Turn < settings.MaxTurns {
		if err := ctx.Err(); err != nil {
			result.Error = err.Error()
			break
		}
		alive := 0
		for _, s := range board.Snakes {
			if s.EliminatedCause == rules.NotEliminated {
				alive++
			}
		}
		if alive == 0 || (settings.Ruleset != "solo" && alive <= 1) {
			break
		}
		board, err = maps.PreUpdateBoard(gameMap, board, engine.Settings())
		if err != nil {
			result.Error = err.Error()
			break
		}
		if job.Trace {
			result.Frames = append(result.Frames, arenaState(board, settings, job, job.Focus))
		}
		states := make([]GameState, len(ids))
		decisions := make([]ArenaDecision, len(ids))
		requestMove := func(i int) {
			start := time.Now()
			var move string
			var err error
			if clients[i] != nil {
				move, err = callSnake(ctx, clients[i], job.Snakes[i].URL, "move", states[i])
			} else {
				move, err = safePolicyContext(ctx, start, states[i], job.Snakes[i])
			}
			elapsed := float64(time.Since(start).Nanoseconds()) / 1e6
			decision := ArenaDecision{Turn: board.Turn, Seat: i, ElapsedMS: elapsed, ResponseMove: move, Timeout: elapsed > float64(settings.TimeoutMS)}
			if err != nil {
				decision.Error = err.Error()
			}
			decisions[i] = decision
		}
		var requests sync.WaitGroup
		for i, snake := range board.Snakes {
			if snake.EliminatedCause != rules.NotEliminated {
				continue
			}
			states[i] = arenaState(board, settings, job, i)
			if concurrentMoves && clients[i] != nil {
				requests.Add(1)
				go func(i int) {
					defer requests.Done()
					requestMove(i)
				}(i)
			} else {
				requestMove(i)
			}
		}
		requests.Wait()
		moves := make([]rules.SnakeMove, 0, len(ids))
		for i, snake := range board.Snakes {
			if snake.EliminatedCause != rules.NotEliminated {
				continue
			}
			decision := decisions[i]
			move, elapsed := decision.ResponseMove, decision.ElapsedMS
			metrics := &result.Snakes[i]
			metrics.Moves++
			metrics.MaxMS = max(metrics.MaxMS, elapsed)
			latencies[i] = append(latencies[i], elapsed)
			valid := slices.Contains([]string{"up", "down", "left", "right"}, move)
			if decision.Error == "" && !valid {
				decision.Error = "invalid move response"
			}
			if elapsed > float64(settings.TimeoutMS) {
				metrics.Timeouts++
				move = previous[i]
			} else if decision.Error != "" || !valid {
				metrics.Errors++
				move = previous[i]
			}
			if move == "" {
				move = arenaDefaultMove(snake)
			}
			if job.Trace {
				decision.Move = move
				result.Decisions = append(result.Decisions, decision)
			}
			previous[i] = move
			moves = append(moves, rules.SnakeMove{ID: snake.ID, Move: move})
			if i == job.Focus {
				result.LastDecision = &states[i]
				result.LastMove = move
			}
		}
		_, board, err = engine.Execute(board, moves)
		if err != nil {
			result.Error = err.Error()
			break
		}
		board, err = maps.PostUpdateBoard(gameMap, board, engine.Settings())
		if err != nil {
			result.Error = err.Error()
			break
		}
		board.Turn++
	}
	result.Turns = board.Turn
	alive := []int{}
	for i, s := range board.Snakes {
		m := &result.Snakes[i]
		m.Length = len(s.Body)
		m.Eliminated = s.EliminatedCause
		m.EliminatedTurn = s.EliminatedOnTurn
		if len(latencies[i]) > 0 {
			slices.Sort(latencies[i])
			m.P95MS = latencies[i][(len(latencies[i])-1)*95/100]
		}
		if s.EliminatedCause == rules.NotEliminated {
			alive = append(alive, i)
		}
		if clients[i] != nil {
			if _, err := callSnake(ctx, clients[i], job.Snakes[i].URL, "end", arenaState(board, settings, job, i)); err != nil {
				m.Errors++
			}
		}
	}
	result.Truncated = board.Turn >= settings.MaxTurns && (len(alive) > 1 || (settings.Ruleset == "solo" && len(alive) > 0))
	if result.Error == "" && len(alive) == 1 && !result.Truncated && settings.Ruleset != "solo" {
		result.Winner = alive[0]
	}
	if job.Trace {
		result.Frames = append(result.Frames, arenaState(board, settings, job, job.Focus))
	}
	return
}

func safePolicy(state GameState, p Policy) (move string, err error) {
	return safePolicyContext(context.Background(), time.Now(), state, p)
}

func safePolicyContext(ctx context.Context, arrived time.Time, state GameState, p Policy) (move string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("policy panic: %v", r)
		}
	}()
	return selectMoveContext(ctx, arrived, state, p), nil
}
func arenaDefaultMove(s rules.Snake) string {
	if len(s.Body) > 1 {
		for _, d := range directions {
			if s.Body[0].X-s.Body[1].X == d.delta.X && s.Body[0].Y-s.Body[1].Y == d.delta.Y {
				return d.name
			}
		}
	}
	return "up"
}
func arenaState(b *rules.BoardState, s ArenaSettings, j ArenaJob, seat int) GameState {
	state := GameState{Game: Game{ID: j.ID, Timeout: s.TimeoutMS, Map: s.Map, Source: "local-league", Ruleset: Ruleset{Name: s.Ruleset, Version: "v1.2.3", Settings: RulesetSettings{FoodSpawnChance: s.FoodSpawnChance, MinimumFood: s.MinimumFood, HazardDamagePerTurn: 14}}}, Turn: b.Turn, Board: Board{Width: b.Width, Height: b.Height, Food: []Coord{}, Hazards: []Coord{}, Snakes: []Battlesnake{}}}
	for _, p := range b.Food {
		state.Board.Food = append(state.Board.Food, Coord{p.X, p.Y})
	}
	for _, p := range b.Hazards {
		state.Board.Hazards = append(state.Board.Hazards, Coord{p.X, p.Y})
	}
	for i, snake := range b.Snakes {
		sn := Battlesnake{ID: snake.ID, Name: j.Snakes[i].Name, Health: snake.Health, Length: len(snake.Body), Body: []Coord{}}
		for _, p := range snake.Body {
			sn.Body = append(sn.Body, Coord{p.X, p.Y})
		}
		if len(sn.Body) > 0 {
			sn.Head = sn.Body[0]
		}
		if snake.EliminatedCause == rules.NotEliminated {
			state.Board.Snakes = append(state.Board.Snakes, sn)
		}
		if i == seat {
			state.You = sn
		}
	}
	return state
}
func validateLocalURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("opponents must use explicit local HTTP URLs")
	}
	return nil
}
func callSnake(ctx context.Context, client *http.Client, base, event string, state GameState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/"+event, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", response.StatusCode)
	}
	const responseLimit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return "", err
	}
	if len(body) > responseLimit {
		return "", errors.New("snake response exceeds 1 MiB")
	}
	if event != "move" {
		return "", nil
	}
	var result BattlesnakeMoveResponse
	err = json.Unmarshal(body, &result)
	return result.Move, err
}

func arenaCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "policies":
		return json.NewEncoder(os.Stdout).Encode(defaultPolicies())
	case "simulate":
		return runArena(ctx, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown command %q (use policies or simulate)", args[0])
	}
}
