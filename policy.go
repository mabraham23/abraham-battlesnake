package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/BattlesnakeOfficial/rules"
)

const maxLookaheadSnakes = 5

type Policy struct {
	Name               string  `json:"name"`
	Kind               string  `json:"kind"`
	URL                string  `json:"url,omitempty"`
	SpaceWeight        float64 `json:"space_weight"`
	FoodWeight         float64 `json:"food_weight"`
	HungerThreshold    float64 `json:"hunger_threshold"`
	TerritoryWeight    float64 `json:"territory_weight"`
	TailWeight         float64 `json:"tail_weight"`
	HeadRisk           float64 `json:"head_risk"`
	TrapPenalty        float64 `json:"trap_penalty"`
	Lookahead          bool    `json:"lookahead"`
	SuccessorLookahead bool    `json:"successor_lookahead,omitempty"`
	SuccessorBudgetMS  int     `json:"successor_budget_ms,omitempty"`
	MobilityWeight     float64 `json:"mobility_weight,omitempty"`
	SurvivalDepth      int     `json:"survival_depth,omitempty"`
	EscapeDepth        int     `json:"escape_depth,omitempty"`
	UncappedTerritory  bool    `json:"uncapped_territory,omitempty"`
	SearchDepth        int     `json:"search_depth,omitempty"`
	SearchBudgetMS     int     `json:"search_budget_ms,omitempty"`
	SearchWeight       float64 `json:"search_weight,omitempty"`
	SearchFoodWeight   float64 `json:"search_food_weight,omitempty"`
}

func defaultPolicies() []Policy {
	return []Policy{
		{Name: "baseline", Kind: "baseline"},
		{Name: "balanced", Kind: "heuristic", SpaceWeight: 6, FoodWeight: 18, HungerThreshold: 40, TerritoryWeight: 2, TailWeight: 25, HeadRisk: 10000, TrapPenalty: 1500},
		{Name: "cautious", Kind: "heuristic", SpaceWeight: 10, FoodWeight: 15, HungerThreshold: 45, TerritoryWeight: 3, TailWeight: 50, HeadRisk: 20000, TrapPenalty: 3000, Lookahead: true},
		{Name: "greedy", Kind: "heuristic", SpaceWeight: 4, FoodWeight: 45, HungerThreshold: 65, TerritoryWeight: 1, TailWeight: 15, HeadRisk: 8000, TrapPenalty: 1500},
		{Name: "aggressive", Kind: "heuristic", SpaceWeight: 5, FoodWeight: 25, HungerThreshold: 40, TerritoryWeight: 8, TailWeight: 20, HeadRisk: 3000, TrapPenalty: 1500},
		{Name: "successor", Kind: "heuristic", SpaceWeight: 10, FoodWeight: 15, HungerThreshold: 45, TerritoryWeight: 3, TailWeight: 50, HeadRisk: 20000, TrapPenalty: 3000, Lookahead: true, SuccessorLookahead: true},
	}
}

func validatePolicy(p Policy) error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 80 {
		return fmt.Errorf("policy name must contain 1 to 80 characters")
	}
	if p.Kind != "baseline" && p.Kind != "heuristic" {
		return fmt.Errorf("policy %q kind must be baseline or heuristic", p.Name)
	}
	for _, value := range []float64{p.SpaceWeight, p.FoodWeight, p.TerritoryWeight, p.TailWeight, p.HeadRisk, p.TrapPenalty, p.MobilityWeight, p.SearchWeight, p.SearchFoodWeight} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100000 {
			return fmt.Errorf("policy %q weights must be finite and between 0 and 100000", p.Name)
		}
	}
	if math.IsNaN(p.HungerThreshold) || math.IsInf(p.HungerThreshold, 0) || p.HungerThreshold < 0 || p.HungerThreshold > 100 {
		return fmt.Errorf("policy %q hunger threshold must be between 0 and 100", p.Name)
	}
	if p.SuccessorBudgetMS < 0 || p.SuccessorBudgetMS > 200 {
		return fmt.Errorf("policy %q successor budget must be between 0 and 200 milliseconds", p.Name)
	}
	if p.SurvivalDepth != 0 && (p.SurvivalDepth < 2 || p.SurvivalDepth > 5) {
		return fmt.Errorf("policy %q survival depth must be zero or between two and five turns", p.Name)
	}
	if p.EscapeDepth != 0 && (p.EscapeDepth < 8 || p.EscapeDepth > 32 || !p.SuccessorLookahead) {
		return fmt.Errorf("policy %q escape depth requires successor lookahead and eight to 32 turns", p.Name)
	}
	if p.SearchDepth != 0 && (p.SearchDepth < 2 || p.SearchDepth > 6) {
		return fmt.Errorf("policy %q search depth must be zero or between two and six turns", p.Name)
	}
	if p.SearchBudgetMS < 0 || p.SearchBudgetMS > searchMaxBudgetMS {
		return fmt.Errorf("policy %q search budget must be between 0 and %d milliseconds", p.Name, searchMaxBudgetMS)
	}
	return nil
}

type policyCandidate struct {
	move  string
	score float64
}

func selectMove(state GameState, p Policy) string {
	return selectMoveContext(context.Background(), time.Now(), state, p)
}

func selectMoveContext(ctx context.Context, arrived time.Time, state GameState, p Policy) string {
	if len(state.You.Body) == 0 || state.Board.Width < 1 || state.Board.Height < 1 || state.Board.Width > 100 || state.Board.Height > 100 {
		return "up"
	}
	budget := newMoveBudget(ctx, arrived, state.Game.Timeout)
	fallback := fallbackMove(state)
	if budget.stopped() {
		return fallback
	}
	if p.Kind == "baseline" {
		return chooseMoveWithBudget(state, budget, fallback)
	}
	food, hazards := make(map[Coord]bool), make(map[Coord]int)
	for i, cell := range state.Board.Food {
		if i%32 == 0 && budget.stopped() {
			return fallback
		}
		food[cell] = true
	}
	for i, cell := range state.Board.Hazards {
		if i%32 == 0 && budget.stopped() {
			return fallback
		}
		hazards[cell]++
	}
	blocked := make(map[Coord]bool)
	for _, snake := range state.Board.Snakes {
		if budget.stopped() {
			return fallback
		}
		if snake.ID == state.You.ID || len(snake.Body) == 0 {
			continue
		}
		for i, cell := range snake.Body[:len(snake.Body)-1] {
			if i%32 == 0 && budget.stopped() {
				return fallback
			}
			blocked[cell] = true
		}
	}
	for i, cell := range state.You.Body[:len(state.You.Body)-1] {
		if i%32 == 0 && budget.stopped() {
			return fallback
		}
		blocked[cell] = true
	}
	var safe map[string]bool
	if p.Lookahead || p.SuccessorLookahead || p.SurvivalDepth > 0 || p.SearchDepth > 0 {
		safe = policySafeMovesWithBudget(state, budget)
		for _, dir := range directions {
			if safe[dir.name] {
				fallback = dir.name
				break
			}
		}
	}
	opponents := policyOpponentDistancesWithBudget(state, hazards, food, budget)
	candidates := make([]policyCandidate, 0, 4)
candidateLoop:
	for _, dir := range directions {
		if budget.stopped() {
			break
		}
		next := Coord{state.You.Head.X + dir.delta.X, state.You.Head.Y + dir.delta.Y}
		if !inside(next, state.Board) || blocked[next] {
			continue
		}
		health := state.You.Health - 1 - hazards[next]*state.Game.Ruleset.Settings.HazardDamagePerTurn
		body := append([]Coord{next}, state.You.Body[:len(state.You.Body)-1]...)
		if food[next] {
			health = 100
			body = append(body, body[len(body)-1])
		}
		if health <= 0 {
			continue
		}
		selfCollision := false
		for i, cell := range body[1:] {
			if i%32 == 0 && budget.stopped() {
				break candidateLoop
			}
			if cell == next {
				selfCollision = true
			}
		}
		if selfCollision {
			continue
		}
		release := make(map[Coord]int)
		for i, cell := range body {
			if i%32 == 0 && budget.stopped() {
				break candidateLoop
			}
			release[cell] = max(release[cell], len(body)-i)
		}
		permanent := make(map[Coord]bool)
		for _, snake := range state.Board.Snakes {
			if budget.stopped() {
				break candidateLoop
			}
			if snake.ID == state.You.ID {
				continue
			}
			for i, cell := range snake.Body {
				if i%32 == 0 && budget.stopped() {
					break candidateLoop
				}
				permanent[cell] = true
			}
		}
		// Release times omit future growth and new trail cells; this is a space estimate, not a survival proof.
		distances := policyDistancesWithBudget(next, state.Board, permanent, release, hazards, food, budget)
		if budget.stopped() {
			break
		}
		space, foodDistance := len(distances), -1
		visited := 0
		for cell, distance := range distances {
			if visited%32 == 0 && budget.stopped() {
				break candidateLoop
			}
			visited++
			if food[cell] && (foodDistance < 0 || distance < foodDistance) {
				foodDistance = distance
			}
		}
		territory := policyTerritoryWithBudget(distances, opponents, next, len(body), food[next], budget)
		if budget.stopped() {
			break
		}
		controlled := min(territory, len(body)*2)
		if p.UncappedTerritory {
			controlled = territory
		}
		score := float64(min(space, len(body)*2))*p.SpaceWeight + float64(controlled)*p.TerritoryWeight
		if space < len(body) {
			score -= p.TrapPenalty * (1 + float64(len(body)-space)/float64(len(body)))
		}
		if _, reachable := distances[body[len(body)-1]]; reachable {
			score += p.TailWeight
		}
		foodWeight := p.FoodWeight
		if float64(state.You.Health) <= p.HungerThreshold {
			foodWeight *= 5
		}
		if foodDistance >= 0 && (foodDistance <= health || food[next]) {
			score += foodWeight / float64(foodDistance+1)
		} else if float64(state.You.Health) <= p.HungerThreshold {
			score -= p.TrapPenalty
		}
		score -= float64(max(0, state.You.Health-1-health)) * p.SpaceWeight
		for i, opponent := range state.Board.Snakes {
			if i%32 == 0 && budget.stopped() {
				break candidateLoop
			}
			if opponent.ID == state.You.ID || len(opponent.Body) < len(state.You.Body) {
				continue
			}
			if opponent.Health <= 1 && !food[next] {
				continue
			}
			if abs(next.X-opponent.Head.X)+abs(next.Y-opponent.Head.Y) == 1 {
				score -= p.HeadRisk
			}
		}
		candidates = append(candidates, policyCandidate{dir.name, score})
	}
	if p.SuccessorLookahead && !budget.stopped() {
		penalties := policySuccessorPenalties(state, candidates, safe, budget, p)
		for i := range candidates {
			candidates[i].score -= penalties[candidates[i].move]
		}
	}
	if p.SearchDepth > 0 && !budget.stopped() {
		values := policySearchValues(state, candidates, safe, budget, p)
		best := math.Inf(-1)
		for _, value := range values {
			best = max(best, value)
		}
		weight := p.SearchWeight
		if weight <= 0 {
			weight = 1
		}
		// The best line stays at zero so only the deficit of worse lines moves the heuristic ranking.
		for i := range candidates {
			if value, found := values[candidates[i].move]; found {
				candidates[i].score += weight * (value - best)
			}
		}
	}
	var survival map[string]bool
	if p.SurvivalDepth > 0 && !budget.stopped() {
		survival = policySurvivalMoves(state, candidates, safe, budget, p.SurvivalDepth)
	}
	preferSurvival := false
	for _, candidate := range candidates {
		if survival[candidate.move] {
			preferSurvival = true
			fallback = candidate.move
		}
	}
	stopped := budget.stopped()
	if len(candidates) == 0 && stopped {
		return fallback
	}
	preferSafe := false
	for _, candidate := range candidates {
		preferSafe = preferSafe || safe[candidate.move]
	}
	bestMove, bestScore := "up", math.Inf(-1)
	if stopped {
		bestMove = fallback
		for _, survives := range safe {
			preferSafe = preferSafe || survives
		}
	}
	for _, candidate := range candidates {
		if preferSafe && !safe[candidate.move] {
			continue
		}
		if preferSurvival && !survival[candidate.move] {
			continue
		}
		if candidate.score > bestScore {
			bestMove, bestScore = candidate.move, candidate.score
		}
	}
	return bestMove
}

func policyDistances(start Coord, board Board, blocked map[Coord]bool, release map[Coord]int, hazards map[Coord]int, food map[Coord]bool) map[Coord]int {
	return policyDistancesWithBudget(start, board, blocked, release, hazards, food, nil)
}

func policyDistancesWithBudget(start Coord, board Board, blocked map[Coord]bool, release map[Coord]int, hazards map[Coord]int, food map[Coord]bool, budget *moveBudget) map[Coord]int {
	distances := map[Coord]int{start: 0}
	queue := []Coord{start}
	for i := 0; i < len(queue); i++ {
		if i%32 == 0 && budget.stopped() {
			return nil
		}
		cell := queue[i]
		arrival := distances[cell] + 1
		for _, dir := range directions {
			next := Coord{cell.X + dir.delta.X, cell.Y + dir.delta.Y}
			if _, seen := distances[next]; seen {
				continue
			}
			if !inside(next, board) || blocked[next] || arrival < release[next] || hazards[next] > 0 && !food[next] {
				continue
			}
			distances[next] = arrival
			queue = append(queue, next)
		}
	}
	return distances
}

type policyOpponent struct {
	length    int
	distances map[Coord]int
}

func policyTerritory(distances map[Coord]int, opponents []policyOpponent, next Coord, length int, ate bool) int {
	return policyTerritoryWithBudget(distances, opponents, next, length, ate, nil)
}

func policyTerritoryWithBudget(distances map[Coord]int, opponents []policyOpponent, next Coord, length int, ate bool, budget *moveBudget) int {
	territory := 0
	visited := 0
	for cell, distance := range distances {
		if visited%32 == 0 && budget.stopped() {
			return 0
		}
		visited++
		owned := true
		for i, opponent := range opponents {
			if i%32 == 0 && budget.stopped() {
				return 0
			}
			otherLength := opponent.length
			if arrival, found := opponent.distances[next]; ate && found && arrival == 1 {
				otherLength++
			}
			if theirs, found := opponent.distances[cell]; found && (theirs < distance+1 || theirs == distance+1 && otherLength >= length) {
				owned = false
				break
			}
		}
		if owned {
			territory++
		}
	}
	return territory
}

func policyOpponentDistances(state GameState, hazards map[Coord]int, food map[Coord]bool) []policyOpponent {
	return policyOpponentDistancesWithBudget(state, hazards, food, nil)
}

func policyOpponentDistancesWithBudget(state GameState, hazards map[Coord]int, food map[Coord]bool, budget *moveBudget) []policyOpponent {
	blocked := make(map[Coord]bool)
	for _, snake := range state.Board.Snakes {
		if budget.stopped() {
			return nil
		}
		for i, cell := range snake.Body {
			if i%32 == 0 && budget.stopped() {
				return nil
			}
			blocked[cell] = true
		}
	}
	var opponents []policyOpponent
	for _, snake := range state.Board.Snakes {
		if budget.stopped() {
			return nil
		}
		if snake.ID == state.You.ID {
			continue
		}
		// Static opponent routes and length ties approximate territory; no opponent policy is assumed.
		opponents = append(opponents, policyOpponent{len(snake.Body), policyDistancesWithBudget(snake.Head, state.Board, blocked, nil, hazards, food, budget)})
	}
	return opponents
}

func policySafeMoves(state GameState) map[string]bool {
	return policySafeMovesWithBudget(state, nil)
}

func policySafeMovesWithBudget(state GameState, budget *moveBudget) map[string]bool {
	return policySafeDirectionsWithBudget(state, budget, directions[:])
}

func policySafeDirectionsWithBudget(state GameState, budget *moveBudget, choices []direction) map[string]bool {
	if state.Game.Ruleset.Name != "" && state.Game.Ruleset.Name != "standard" && state.Game.Ruleset.Name != "solo" {
		return nil
	}
	// Five snakes bound the filter to 1,024 one-turn transitions, with the request deadline still enforced.
	if len(state.Board.Snakes) > maxLookaheadSnakes || len(state.Board.Snakes) == 0 || budget.stopped() {
		return nil
	}
	board := policyRulesBoardWithBudget(state, budget)
	if board == nil {
		return nil
	}
	ruleset := rules.NewRulesetBuilder().WithSolo(true).WithParams(map[string]string{
		rules.ParamHazardDamagePerTurn: strconv.Itoa(state.Game.Ruleset.Settings.HazardDamagePerTurn),
	}).NamedRuleset(rules.GameTypeStandard)
	moves := make([]rules.SnakeMove, len(board.Snakes))
	for i, snake := range board.Snakes {
		if len(snake.Body) == 0 {
			return nil
		}
		moves[i].ID = snake.ID
	}
	safe := make(map[string]bool, 4)
	for _, dir := range choices {
		failed := false
		var survives func(int) bool
		survives = func(index int) bool {
			if budget.stopped() {
				return false
			}
			if index == len(moves) {
				_, next, err := ruleset.Execute(board, moves)
				if err != nil {
					failed = true
					return false
				}
				for _, snake := range next.Snakes {
					if snake.ID == state.You.ID {
						return snake.EliminatedCause == rules.NotEliminated
					}
				}
				return false
			}
			if moves[index].ID == state.You.ID {
				moves[index].Move = dir.name
				return survives(index + 1)
			}
			for _, reply := range directions {
				moves[index].Move = reply.name
				if !survives(index + 1) {
					return false
				}
			}
			return true
		}
		survived := survives(0)
		if failed {
			return nil
		}
		if budget.stopped() {
			return safe
		}
		safe[dir.name] = survived
	}
	return safe
}

func policyRulesBoard(state GameState) *rules.BoardState {
	return policyRulesBoardWithBudget(state, nil)
}

func policyRulesBoardWithBudget(state GameState, budget *moveBudget) *rules.BoardState {
	board := rules.NewBoardState(state.Board.Width, state.Board.Height)
	board.Turn = state.Turn
	for i, cell := range state.Board.Food {
		if i%32 == 0 && budget.stopped() {
			return nil
		}
		board.Food = append(board.Food, rules.Point{X: cell.X, Y: cell.Y})
	}
	for i, cell := range state.Board.Hazards {
		if i%32 == 0 && budget.stopped() {
			return nil
		}
		board.Hazards = append(board.Hazards, rules.Point{X: cell.X, Y: cell.Y})
	}
	for _, snake := range state.Board.Snakes {
		if budget.stopped() {
			return nil
		}
		if snake.ID == state.You.ID {
			snake = state.You
		}
		converted := rules.Snake{ID: snake.ID, Health: snake.Health}
		for i, cell := range snake.Body {
			if i%32 == 0 && budget.stopped() {
				return nil
			}
			converted.Body = append(converted.Body, rules.Point{X: cell.X, Y: cell.Y})
		}
		board.Snakes = append(board.Snakes, converted)
	}
	return board
}
