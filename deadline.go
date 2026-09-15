package main

import (
	"context"
	"time"
)

type moveBudget struct {
	ctx      context.Context
	deadline time.Time
}

func newMoveBudget(ctx context.Context, arrived time.Time, timeoutMS int) *moveBudget {
	if timeoutMS <= 0 {
		timeoutMS = 500
	}
	timeout := time.Duration(min(timeoutMS, int((1<<63-1)/time.Millisecond))) * time.Millisecond
	// Leave time for response encoding and transit; very small budgets use only the fallback.
	margin := min(timeout, max(5*time.Millisecond, min(timeout/10, 50*time.Millisecond)))
	deadline := arrived.Add(timeout - margin)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	return &moveBudget{ctx: ctx, deadline: deadline}
}

func (b *moveBudget) stopped() bool {
	return b != nil && (b.ctx.Err() != nil || !time.Now().Before(b.deadline))
}

func fallbackMove(state GameState) string {
	best := "up"
	found := false
	for _, dir := range directions {
		next := Coord{state.You.Head.X + dir.delta.X, state.You.Head.Y + dir.delta.Y}
		if !inside(next, state.Board) {
			continue
		}
		blocked := len(state.You.Body) > 1 && next == state.You.Body[1]
		for _, snake := range state.Board.Snakes {
			if snake.ID == state.You.ID {
				continue
			}
			for i := 0; i+1 < len(snake.Body); i++ {
				blocked = blocked || snake.Body[i] == next
			}
		}
		for i := 0; i+1 < len(state.You.Body); i++ {
			blocked = blocked || state.You.Body[i] == next
		}
		if blocked {
			continue
		}
		food := false
		for _, cell := range state.Board.Food {
			food = food || cell == next
		}
		health := state.You.Health - 1
		for _, cell := range state.Board.Hazards {
			if cell == next {
				health -= state.Game.Ruleset.Settings.HazardDamagePerTurn
			}
		}
		if food {
			health = 100
		}
		if health <= 0 {
			continue
		}
		if !found {
			best, found = dir.name, true
		}
		risky := false
		for _, snake := range state.Board.Snakes {
			if snake.ID == state.You.ID || len(snake.Body) < len(state.You.Body) || snake.Health <= 1 && !food {
				continue
			}
			if abs(next.X-snake.Head.X)+abs(next.Y-snake.Head.Y) == 1 {
				risky = true
			}
		}
		if !risky {
			return dir.name
		}
	}
	return best
}
