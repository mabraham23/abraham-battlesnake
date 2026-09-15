package main

type direction struct {
	name  string
	delta Coord
}

var directions = [...]direction{
	{"up", Coord{0, 1}},
	{"right", Coord{1, 0}},
	{"down", Coord{0, -1}},
	{"left", Coord{-1, 0}},
}

func chooseMove(state GameState) string {
	return chooseMoveWithBudget(state, nil, "up")
}

func chooseMoveWithBudget(state GameState, budget *moveBudget, fallback string) string {
	blocked := make(map[Coord]bool)
	for _, snake := range state.Board.Snakes {
		if budget.stopped() {
			return fallback
		}
		if snake.ID == state.You.ID {
			continue
		}
		for i, p := range snake.Body {
			if i%32 == 0 && budget.stopped() {
				return fallback
			}
			blocked[p] = true
		}
	}
	// Movement removes the last segment; a duplicated tail still has an occupied segment.
	for i, p := range state.You.Body[:len(state.You.Body)-1] {
		if i%32 == 0 && budget.stopped() {
			return fallback
		}
		blocked[p] = true
	}
	if len(state.You.Body) > 1 {
		blocked[state.You.Body[1]] = true
	}

	food := make(map[Coord]bool)
	for i, p := range state.Board.Food {
		if i%32 == 0 && budget.stopped() {
			return fallback
		}
		food[p] = true
	}
	hazards := make(map[Coord]int)
	for i, p := range state.Board.Hazards {
		if i%32 == 0 && budget.stopped() {
			return fallback
		}
		hazards[p]++
	}

	bestMove, bestScore := "up", -int(^uint(0)>>1)
	completed := false
	for _, dir := range directions {
		if budget.stopped() {
			if completed {
				return bestMove
			}
			return fallback
		}
		next := Coord{state.You.Head.X + dir.delta.X, state.You.Head.Y + dir.delta.Y}
		if !inside(next, state.Board) || blocked[next] {
			continue
		}
		health := state.You.Health - 1
		length := len(state.You.Body)
		damage := hazards[next] * state.Game.Ruleset.Settings.HazardDamagePerTurn
		if food[next] {
			health = 100
			length++
			damage = 0
		} else {
			health -= damage
		}
		if health <= 0 {
			continue
		}

		space, distance := exploreWithBudget(next, state.Board, blocked, food, hazards, budget)
		if budget.stopped() {
			if completed {
				return bestMove
			}
			return fallback
		}
		score := min(space, length*2)*10 - damage*10
		if space < length {
			score -= 10000 + (length-space)*100
		}
		for _, opponent := range state.Board.Snakes {
			if opponent.ID == state.You.ID || len(opponent.Body) < len(state.You.Body) {
				continue
			}
			if abs(next.X-opponent.Head.X)+abs(next.Y-opponent.Head.Y) == 1 {
				score -= 100000
			}
		}
		foodWeight := 10
		if state.You.Health < 40 {
			foodWeight = 100
		}
		// Food on the last available health turn still saves us.
		if distance >= 0 && (distance <= health || food[next]) {
			score += (state.Board.Width + state.Board.Height - distance) * foodWeight
		} else if state.You.Health < 40 {
			score -= 5000
		}
		if score > bestScore {
			bestMove, bestScore = dir.name, score
		}
		completed = true
	}
	return bestMove
}

func explore(start Coord, board Board, blocked, food map[Coord]bool, hazards map[Coord]int) (int, int) {
	return exploreWithBudget(start, board, blocked, food, hazards, nil)
}

func exploreWithBudget(start Coord, board Board, blocked, food map[Coord]bool, hazards map[Coord]int, budget *moveBudget) (int, int) {
	distances := map[Coord]int{start: 0}
	queue := []Coord{start}
	foodDistance := -1
	for i := 0; i < len(queue); i++ {
		if i%32 == 0 && budget.stopped() {
			return 0, -1
		}
		p := queue[i]
		if food[p] && foodDistance == -1 {
			foodDistance = distances[p]
		}
		for _, dir := range directions {
			next := Coord{p.X + dir.delta.X, p.Y + dir.delta.Y}
			if _, seen := distances[next]; seen {
				continue
			}
			if !inside(next, board) || blocked[next] || (hazards[next] > 0 && !food[next]) {
				continue
			}
			distances[next] = distances[p] + 1
			queue = append(queue, next)
		}
	}
	return len(queue), foodDistance
}

func inside(p Coord, board Board) bool {
	return p.X >= 0 && p.Y >= 0 && p.X < board.Width && p.Y < board.Height
}

func abs(n int) int {
	return max(n, -n)
}
