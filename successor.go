package main

import (
	"strconv"
	"time"

	"github.com/BattlesnakeOfficial/rules"
)

func policySuccessorPenalties(state GameState, candidates []policyCandidate, safe map[string]bool, requestBudget *moveBudget, policy Policy) map[string]float64 {
	timeoutMS := policy.SuccessorBudgetMS
	if timeoutMS <= 0 {
		timeoutMS = 50
	}
	deadline := time.Now().Add(time.Duration(min(timeoutMS, 200)) * time.Millisecond)
	if requestBudget.deadline.Before(deadline) {
		deadline = requestBudget.deadline
	}
	budget := &moveBudget{ctx: requestBudget.ctx, deadline: deadline}
	board := policyRulesBoardWithBudget(state, budget)
	if board == nil || len(board.Snakes) == 0 || len(board.Snakes) > 4 {
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
	penalties := make(map[string]float64)
	for _, candidate := range candidates {
		if !safe[candidate.move] {
			continue
		}
		worstPenalty := 0.0
		var replies func(int) bool
		replies = func(index int) bool {
			if budget.stopped() {
				return false
			}
			if index == len(moves) {
				_, next, err := ruleset.Execute(board, moves)
				if err != nil {
					return false
				}
				successor, alive := policySuccessorState(state, next, budget)
				if !alive {
					return false
				}
				if len(successor.Board.Snakes) == 1 && len(state.Board.Snakes) > 1 && state.Game.Ruleset.Name != "solo" {
					return true
				}
				penalty, exits, complete := policySuccessorEscape(successor, budget, policy.MobilityWeight > 0)
				cost := policy.TrapPenalty*penalty + policy.MobilityWeight*float64(max(0, 3-exits))
				worstPenalty = max(worstPenalty, cost)
				return complete
			}
			if moves[index].ID == state.You.ID {
				moves[index].Move = candidate.move
				return replies(index + 1)
			}
			// Collision eliminations are simultaneous, so even a self-colliding reply can affect us.
			for _, reply := range directions {
				moves[index].Move = reply.name
				if !replies(index + 1) {
					return false
				}
			}
			return true
		}
		if !replies(0) || budget.stopped() {
			// Discard the whole supplemental pass rather than favoring unexamined moves.
			return nil
		}
		penalties[candidate.move] = worstPenalty
	}
	return penalties
}

func policySuccessorEscape(state GameState, budget *moveBudget, countExits bool) (float64, int, bool) {
	food, hazards := make(map[Coord]bool), make(map[Coord]int)
	for i, cell := range state.Board.Food {
		if i%32 == 0 && budget.stopped() {
			return 0, 0, false
		}
		food[cell] = true
	}
	for i, cell := range state.Board.Hazards {
		if i%32 == 0 && budget.stopped() {
			return 0, 0, false
		}
		hazards[cell]++
	}
	blocked := make(map[Coord]bool)
	for _, snake := range state.Board.Snakes {
		if snake.ID != state.You.ID {
			for i, cell := range snake.Body {
				if i%32 == 0 && budget.stopped() {
					return 0, 0, false
				}
				blocked[cell] = true
			}
		}
	}
	best := 2.0
	exits := 0
	for _, dir := range directions {
		if budget.stopped() {
			return 0, 0, false
		}
		if !policySafeDirectionsWithBudget(state, budget, []direction{dir})[dir.name] {
			continue
		}
		exits++
		if best == 0 {
			continue
		}
		next := Coord{state.You.Head.X + dir.delta.X, state.You.Head.Y + dir.delta.Y}
		body := append([]Coord{next}, state.You.Body[:len(state.You.Body)-1]...)
		if food[next] {
			body = append(body, body[len(body)-1])
		}
		release := make(map[Coord]int)
		for i, cell := range body {
			if i%32 == 0 && budget.stopped() {
				return 0, 0, false
			}
			release[cell] = max(release[cell], len(body)-i)
		}
		// Moving-tail space omits future trail and growth; only the immediate safety check is exact.
		space := len(policyDistancesWithBudget(next, state.Board, blocked, release, hazards, food, budget))
		if space >= len(body) {
			if !countExits {
				return 0, exits, !budget.stopped()
			}
			best = 0
			continue
		}
		best = min(best, 1+float64(len(body)-space)/float64(len(body)))
	}
	return best, exits, !budget.stopped()
}

func policySuccessorState(previous GameState, board *rules.BoardState, budget *moveBudget) (GameState, bool) {
	state := GameState{Game: previous.Game, Turn: previous.Turn + 1, Board: Board{Width: board.Width, Height: board.Height}}
	for i, cell := range board.Food {
		if i%32 == 0 && budget.stopped() {
			return state, false
		}
		state.Board.Food = append(state.Board.Food, Coord{cell.X, cell.Y})
	}
	for i, cell := range board.Hazards {
		if i%32 == 0 && budget.stopped() {
			return state, false
		}
		state.Board.Hazards = append(state.Board.Hazards, Coord{cell.X, cell.Y})
	}
	for _, snake := range board.Snakes {
		if budget.stopped() {
			return state, false
		}
		if snake.EliminatedCause != rules.NotEliminated || len(snake.Body) == 0 {
			continue
		}
		converted := Battlesnake{ID: snake.ID, Health: snake.Health, Length: len(snake.Body), Head: Coord{snake.Body[0].X, snake.Body[0].Y}}
		for i, cell := range snake.Body {
			if i%32 == 0 && budget.stopped() {
				return state, false
			}
			converted.Body = append(converted.Body, Coord{cell.X, cell.Y})
		}
		state.Board.Snakes = append(state.Board.Snakes, converted)
		if snake.ID == previous.You.ID {
			state.You = converted
		}
	}
	return state, len(state.You.Body) > 0
}
