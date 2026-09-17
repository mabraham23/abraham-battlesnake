package main

type routeSearch struct {
	board   Board
	food    map[Coord]bool
	hazards map[Coord]int
	blocked map[Coord]bool
	damage  int
	budget  *moveBudget
	nodes   int
}

func policyRouteEscape(state GameState, budget *moveBudget, depth int) (float64, bool) {
	search := routeSearch{
		board: state.Board, food: make(map[Coord]bool), hazards: make(map[Coord]int),
		blocked: make(map[Coord]bool), damage: state.Game.Ruleset.Settings.HazardDamagePerTurn, budget: budget,
	}
	for _, cell := range state.Board.Food {
		search.food[cell] = true
	}
	for _, cell := range state.Board.Hazards {
		search.hazards[cell]++
	}
	for _, snake := range state.Board.Snakes {
		if snake.ID != state.You.ID {
			for _, cell := range snake.Body {
				search.blocked[cell] = true
			}
		}
	}
	safe := policySafeMovesWithBudget(state, budget)
	best, unknown := 0, false
	for _, dir := range directions {
		if !safe[dir.name] {
			continue
		}
		reached := search.route(state.You.Body, state.You.Health, depth, dir.name)
		if budget.stopped() {
			return 0, false
		}
		if reached == depth {
			return 0, true
		}
		unknown = unknown || reached < 0
		best = max(best, reached)
	}
	if unknown {
		return 0, false
	}
	return float64(depth - best), !budget.stopped()
}

// Opponent bodies remain fixed; this is a route heuristic, not a proof against future replies.
func (s *routeSearch) route(body []Coord, health, remaining int, forced string) int {
	if s.budget.stopped() || s.nodes >= 4096 {
		return -1
	}
	if remaining == 0 {
		return 0
	}
	s.nodes++
	best, unknown := 0, false
	for _, dir := range directions {
		if forced != "" && dir.name != forced {
			continue
		}
		next := Coord{body[0].X + dir.delta.X, body[0].Y + dir.delta.Y}
		if !inside(next, s.board) || s.blocked[next] {
			continue
		}
		collision := false
		for _, cell := range body[:len(body)-1] {
			collision = collision || next == cell
		}
		if collision {
			continue
		}
		ate := s.food[next]
		nextHealth := health - 1 - s.hazards[next]*s.damage
		if ate {
			nextHealth = 100
		}
		if nextHealth <= 0 {
			continue
		}
		nextBody := make([]Coord, len(body), len(body)+1)
		nextBody[0] = next
		copy(nextBody[1:], body[:len(body)-1])
		if ate {
			nextBody = append(nextBody, nextBody[len(nextBody)-1])
			delete(s.food, next)
		}
		reached := s.route(nextBody, nextHealth, remaining-1, "")
		if ate {
			s.food[next] = true
		}
		if reached == remaining-1 {
			return remaining
		}
		unknown = unknown || reached < 0
		if reached >= 0 {
			best = max(best, 1+reached)
		}
	}
	if unknown {
		return -1
	}
	return best
}
