package main

import (
	"math"
	"strconv"
	"time"

	"github.com/BattlesnakeOfficial/rules"
)

const (
	searchDefaultBudgetMS = 150
	searchMaxBudgetMS     = 300
	searchMaxNodes        = 20000
	searchDeathValue      = -1e6
	searchWinValue        = 1e5
)

// Paranoid minimax: we maximize while every opponent jointly minimizes our leaf evaluation.
func policySearchValues(state GameState, candidates []policyCandidate, safe map[string]bool, requestBudget *moveBudget, p Policy) map[string]float64 {
	choices := make([]string, 0, 4)
	for _, candidate := range candidates {
		if safe[candidate.move] {
			choices = append(choices, candidate.move)
		}
	}
	if len(choices) < 2 || p.SearchDepth < 2 || p.SearchDepth > 6 || requestBudget.stopped() {
		return nil
	}
	timeoutMS := p.SearchBudgetMS
	if p.SearchBudgetDuelMS > 0 && policyAliveSnakes(state) == 2 {
		timeoutMS = p.SearchBudgetDuelMS
	}
	if timeoutMS <= 0 {
		timeoutMS = searchDefaultBudgetMS
	}
	deadline := time.Now().Add(time.Duration(min(timeoutMS, searchMaxBudgetMS)) * time.Millisecond)
	if requestBudget.deadline.Before(deadline) {
		deadline = requestBudget.deadline
	}
	budget := &moveBudget{ctx: requestBudget.ctx, deadline: deadline}
	board := policyRulesBoardWithBudget(state, budget)
	if board == nil || len(board.Snakes) == 0 || len(board.Snakes) > maxLookaheadSnakes {
		return nil
	}
	for _, snake := range board.Snakes {
		if len(snake.Body) == 0 {
			return nil
		}
	}
	search := newParanoidSearch(state, board, budget, p)
	depth := p.SearchDepth
	if p.SearchDepthDuel > 0 && len(board.Snakes) == 2 {
		// Two-snake branching is small enough for a deeper horizon within the same node cap and deadline.
		depth = p.SearchDepthDuel
	}
	var completed map[string]float64
	for horizon := 2; horizon <= depth; horizon++ {
		current := make(map[string]float64, len(choices))
		for _, move := range choices {
			value := search.maxNode(board, horizon, 0, move, math.Inf(-1), math.Inf(1))
			if search.aborted {
				// Only compare moves at a horizon completed for every alternative.
				return completed
			}
			current[move] = value
		}
		completed = current
	}
	return completed
}

type paranoidSearch struct {
	budget          *moveBudget
	ruleset         rules.Ruleset
	you             string
	solo            bool
	width, height   int
	spaceWeight     float64
	territoryWeight float64
	foodWeight      float64
	lengthWeight    float64
	edgePenalty     float64
	chokeWeight     float64
	maxNodes        int
	duelBranching   bool
	earlyGrowth     bool
	hunger          int
	opponents       int
	nodes           int
	aborted         bool
	hazard          []bool
	food            []bool
	occupied        []bool
	release         []int32
	ours            []int32
	theirs          []int32
	theirLen        []int32
	queue           []int32
}

func newParanoidSearch(state GameState, board *rules.BoardState, budget *moveBudget, p Policy) *paranoidSearch {
	cells := board.Width * board.Height
	s := &paranoidSearch{
		budget: budget, you: state.You.ID, solo: state.Game.Ruleset.Name == "solo" || len(board.Snakes) == 1,
		width: board.Width, height: board.Height, spaceWeight: p.SpaceWeight, territoryWeight: p.TerritoryWeight,
		foodWeight: p.SearchFoodWeight, lengthWeight: 50, earlyGrowth: p.SearchLengthWeight > 0, hunger: int(p.HungerThreshold),
		opponents: len(board.Snakes) - 1, edgePenalty: p.SearchEdgePenalty, chokeWeight: p.SearchChokeWeight, maxNodes: searchMaxNodes, duelBranching: p.SearchBudgetDuelMS > 0 && len(board.Snakes) == 2,
		ruleset: rules.NewRulesetBuilder().WithSolo(true).WithParams(map[string]string{
			rules.ParamFoodSpawnChance: "0", rules.ParamMinimumFood: "0",
			rules.ParamHazardDamagePerTurn: strconv.Itoa(state.Game.Ruleset.Settings.HazardDamagePerTurn),
		}).NamedRuleset(rules.GameTypeStandard),
		hazard: make([]bool, cells), food: make([]bool, cells), occupied: make([]bool, cells),
		release: make([]int32, cells), ours: make([]int32, cells), theirs: make([]int32, cells), theirLen: make([]int32, cells),
		queue: make([]int32, 0, cells),
	}
	if p.SearchLengthWeight > 0 {
		s.lengthWeight = p.SearchLengthWeight
	}
	if p.SearchNodes > 0 {
		s.maxNodes = p.SearchNodes
	}
	for _, cell := range board.Hazards {
		if s.inside(cell) {
			s.hazard[s.index(cell)] = true
		}
	}
	return s
}

func (s *paranoidSearch) inside(cell rules.Point) bool {
	return cell.X >= 0 && cell.Y >= 0 && cell.X < s.width && cell.Y < s.height
}

func (s *paranoidSearch) index(cell rules.Point) int {
	return cell.Y*s.width + cell.X
}

func (s *paranoidSearch) maxNode(board *rules.BoardState, remaining, ply int, forced string, alpha, beta float64) float64 {
	if s.aborted || s.nodes >= s.maxNodes || s.budget.stopped() {
		s.aborted = true
		return 0
	}
	live := make([]rules.Snake, 0, len(board.Snakes))
	us := -1
	for _, snake := range board.Snakes {
		if snake.EliminatedCause != rules.NotEliminated || len(snake.Body) == 0 {
			continue
		}
		if snake.ID == s.you {
			us = len(live)
		}
		live = append(live, snake)
	}
	if us < 0 {
		return searchDeathValue + 1000*float64(ply)
	}
	if remaining == 0 || len(live) == 1 && !s.solo {
		return s.evaluate(board, live, us, ply)
	}
	current := *board
	current.Snakes = live
	s.markOccupied(live)
	var ownMoves [4]string
	count := 0
	if forced != "" {
		ownMoves[0], count = forced, 1
	} else {
		count = s.legalMoves(live[us], &ownMoves)
	}
	best := math.Inf(-1)
	for _, move := range ownMoves[:count] {
		value := s.minNode(&current, live, us, remaining, ply, move, alpha, beta)
		if s.aborted {
			return 0
		}
		best = max(best, value)
		alpha = max(alpha, value)
		if alpha >= beta {
			break
		}
	}
	return best
}

func (s *paranoidSearch) minNode(board *rules.BoardState, live []rules.Snake, us, remaining, ply int, ourMove string, alpha, beta float64) float64 {
	var options [maxLookaheadSnakes][4]string
	var counts [maxLookaheadSnakes]int
	// Option lists are fixed before recursion because deeper nodes reuse the occupancy grid.
	s.markOccupied(live)
	for i, snake := range live {
		if i == us {
			continue
		}
		counts[i] = s.opponentMoves(snake, live[us].Body[0], remaining, &options[i])
	}
	moves := make([]rules.SnakeMove, len(live))
	worst := math.Inf(1)
	var replies func(int) bool
	replies = func(index int) bool {
		if index == len(live) {
			s.nodes++
			_, next, err := s.ruleset.Execute(board, moves)
			if err != nil {
				s.aborted = true
				return false
			}
			value := s.maxNode(next, remaining-1, ply+1, "", alpha, beta)
			if s.aborted {
				return false
			}
			worst = min(worst, value)
			beta = min(beta, value)
			return beta > alpha
		}
		moves[index].ID = live[index].ID
		if index == us {
			moves[index].Move = ourMove
			return replies(index + 1)
		}
		for _, move := range options[index][:counts[index]] {
			moves[index].Move = move
			if !replies(index + 1) {
				return false
			}
		}
		return true
	}
	replies(0)
	return worst
}

// Cells that stay occupied after this turn: every segment except an unstacked tail.
func (s *paranoidSearch) markOccupied(live []rules.Snake) {
	for i := range s.occupied {
		s.occupied[i] = false
	}
	for _, snake := range live {
		for _, cell := range snake.Body[:len(snake.Body)-1] {
			if s.inside(cell) {
				s.occupied[s.index(cell)] = true
			}
		}
	}
}

func (s *paranoidSearch) legalMoves(snake rules.Snake, out *[4]string) int {
	head := snake.Body[0]
	count := 0
	fallback := ""
	for _, dir := range directions {
		next := rules.Point{X: head.X + dir.delta.X, Y: head.Y + dir.delta.Y}
		if !s.inside(next) || len(snake.Body) > 1 && next == snake.Body[1] {
			continue
		}
		if fallback == "" {
			fallback = dir.name
		}
		if s.occupied[s.index(next)] {
			continue
		}
		out[count] = dir.name
		count++
	}
	if count == 0 {
		if fallback == "" {
			fallback = "up"
		}
		out[0], count = fallback, 1
	}
	return count
}

func (s *paranoidSearch) opponentMoves(snake rules.Snake, ourHead rules.Point, remaining int, out *[4]string) int {
	head := snake.Body[0]
	if !s.duelBranching && abs(head.X-ourHead.X)+abs(head.Y-ourHead.Y) > 2*remaining+1 {
		// Distant opponents cannot interact within the horizon; they continue straight when possible.
		if len(snake.Body) > 1 && snake.Body[1] != head {
			straight := rules.Point{X: 2*head.X - snake.Body[1].X, Y: 2*head.Y - snake.Body[1].Y}
			if s.inside(straight) && !s.occupied[s.index(straight)] {
				for _, dir := range directions {
					if head.X+dir.delta.X == straight.X && head.Y+dir.delta.Y == straight.Y {
						out[0] = dir.name
						return 1
					}
				}
			}
		}
		return min(s.legalMoves(snake, out), 1)
	}
	count := s.legalMoves(snake, out)
	// Closest replies first improves alpha-beta cutoffs.
	var distance [4]int
	for i, move := range out[:count] {
		for _, dir := range directions {
			if dir.name == move {
				distance[i] = abs(head.X+dir.delta.X-ourHead.X) + abs(head.Y+dir.delta.Y-ourHead.Y)
			}
		}
	}
	for i := 1; i < count; i++ {
		for j := i; j > 0 && distance[j] < distance[j-1]; j-- {
			distance[j], distance[j-1] = distance[j-1], distance[j]
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return count
}

func (s *paranoidSearch) evaluate(board *rules.BoardState, live []rules.Snake, us, ply int) float64 {
	me := live[us]
	score := 5000 * float64(max(0, s.opponents-(len(live)-1)))
	if len(live) == 1 && !s.solo {
		return searchWinValue + score
	}
	for i := range s.release {
		s.release[i], s.ours[i], s.theirs[i], s.theirLen[i], s.food[i] = 0, -1, -1, 0, false
	}
	for _, cell := range board.Food {
		if s.inside(cell) {
			s.food[s.index(cell)] = true
		}
	}
	maxOpponent := 0
	for i, snake := range live {
		if i != us {
			maxOpponent = max(maxOpponent, len(snake.Body))
		}
		for j, cell := range snake.Body {
			if s.inside(cell) {
				s.release[s.index(cell)] = max(s.release[s.index(cell)], int32(len(snake.Body)-j))
			}
		}
	}
	head := int32(s.index(me.Body[0]))
	s.ours[head] = 0
	queue := append(s.queue[:0], head)
	space, foodDistance := 0, int32(-1)
	for qi := 0; qi < len(queue); qi++ {
		cell := queue[qi]
		distance := s.ours[cell]
		space++
		if s.food[cell] && (foodDistance < 0 || distance < foodDistance) {
			foodDistance = distance
		}
		for _, next := range s.neighbors(cell) {
			if next < 0 || s.ours[next] >= 0 || distance+1 < s.release[next] || s.hazard[next] && !s.food[next] {
				continue
			}
			s.ours[next] = distance + 1
			queue = append(queue, next)
		}
	}
	queue = queue[:0]
	for i, snake := range live {
		if i == us {
			continue
		}
		start := int32(s.index(snake.Body[0]))
		if s.theirs[start] < 0 {
			s.theirs[start] = 0
			queue = append(queue, start)
		}
		s.theirLen[start] = max(s.theirLen[start], int32(len(snake.Body)))
	}
	for qi := 0; qi < len(queue); qi++ {
		cell := queue[qi]
		distance, length := s.theirs[cell], s.theirLen[cell]
		for _, next := range s.neighbors(cell) {
			if next < 0 || distance+1 < s.release[next] || s.hazard[next] && !s.food[next] {
				continue
			}
			if s.theirs[next] < 0 {
				s.theirs[next] = distance + 1
				s.theirLen[next] = length
				queue = append(queue, next)
			} else if s.theirs[next] == distance+1 {
				s.theirLen[next] = max(s.theirLen[next], length)
			}
		}
	}
	s.queue = queue[:0]
	length := int32(len(me.Body))
	territory := 0
	for cell, distance := range s.ours {
		if distance < 0 {
			continue
		}
		if theirs := s.theirs[cell]; theirs < 0 || distance < theirs || distance == theirs && length > s.theirLen[cell] {
			territory++
		}
	}
	score += s.spaceWeight*float64(min(space, 2*len(me.Body))) + s.territoryWeight*float64(territory) + s.lengthWeight*float64(len(me.Body)-maxOpponent)
	if s.chokeWeight > 0 {
		score -= s.chokePenalty(live, us, territory)
	}
	if me.Health < 30 && (foodDistance < 0 || int(foodDistance) > me.Health) {
		score -= 100
	}
	if s.edgePenalty > 0 {
		// Opponents herd us along walls; penalise the edge fully and the next ring by half.
		x, y := me.Body[0].X, me.Body[0].Y
		if x == 0 || y == 0 || x == s.width-1 || y == s.height-1 {
			score -= s.edgePenalty
		} else if x == 1 || y == 1 || x == s.width-2 || y == s.height-2 {
			score -= s.edgePenalty / 2
		}
	}
	// Eat when hungry or not the longest snake; head contests are decided by length.
	if s.foodWeight > 0 && foodDistance >= 0 && (me.Health <= s.hunger+20 || len(me.Body) <= maxOpponent || s.earlyGrowth && len(me.Body) < 8) {
		score += s.foodWeight / float64(foodDistance+1)
	}
	return score
}

// Our territory touching the opponent's along at most two cells is a region the opponent can seal
// beyond the search horizon; penalise it when it is too small to outlive the seal.
func (s *paranoidSearch) chokePenalty(live []rules.Snake, us, territory int) float64 {
	length := int32(len(live[us].Body))
	if territory >= 2*int(length) || len(live) < 2 {
		return 0
	}
	oppHead := int32(-1)
	if len(live) == 2 {
		oppHead = int32(s.index(live[1-us].Body[0]))
	}
	oursCell := func(cell int32) bool {
		distance := s.ours[cell]
		if distance < 0 {
			return false
		}
		theirs := s.theirs[cell]
		return theirs < 0 || distance < theirs || distance == theirs && length > s.theirLen[cell]
	}
	frontier := 0
	// A region nobody can reach is sealed shut, not sealable; the space term already prices it.
	opponentAdjacent := false
	for cell := range s.ours {
		if !oursCell(int32(cell)) {
			continue
		}
		for _, next := range s.neighbors(int32(cell)) {
			if next < 0 {
				continue
			}
			if s.theirs[next] >= 0 {
				opponentAdjacent = true
			}
			// A frontier cell is currently free, opponent-reachable and not ours-first (ties count: they can take it).
			contested := s.release[next] == 0 && s.theirs[next] >= 0 && !oursCell(next)
			if !contested && oppHead >= 0 {
				for _, around := range s.neighbors(oppHead) {
					if around == next {
						contested = true
						break
					}
				}
			}
			if contested {
				frontier++
				break
			}
		}
	}
	if frontier > 2 || !opponentAdjacent {
		return 0
	}
	penalty := s.chokeWeight * (1 + float64(2*int(length)-territory)/float64(2*length))
	if frontier <= 1 && territory < int(length) {
		penalty *= 2
	}
	return penalty
}

func policyAliveSnakes(state GameState) int {
	alive := 0
	for _, snake := range state.Board.Snakes {
		if len(snake.Body) > 0 {
			alive++
		}
	}
	return alive
}

func (s *paranoidSearch) neighbors(cell int32) [4]int32 {
	x, y := int(cell)%s.width, int(cell)/s.width
	next := [4]int32{-1, -1, -1, -1}
	if y+1 < s.height {
		next[0] = cell + int32(s.width)
	}
	if x+1 < s.width {
		next[1] = cell + 1
	}
	if y > 0 {
		next[2] = cell - int32(s.width)
	}
	if x > 0 {
		next[3] = cell - 1
	}
	return next
}
