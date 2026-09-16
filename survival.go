package main

import (
	"strconv"
	"time"

	"github.com/BattlesnakeOfficial/rules"
)

func policySurvivalMoves(state GameState, candidates []policyCandidate, safe map[string]bool, requestBudget *moveBudget, depth int) map[string]bool {
	choices := make([]string, 0, 4)
	for _, candidate := range candidates {
		if safe[candidate.move] {
			choices = append(choices, candidate.move)
		}
	}
	if len(choices) < 2 || depth < 2 || depth > 5 || requestBudget.stopped() {
		return nil
	}
	deadline := time.Now().Add(50 * time.Millisecond)
	if requestBudget.deadline.Before(deadline) {
		deadline = requestBudget.deadline
	}
	budget := &moveBudget{ctx: requestBudget.ctx, deadline: deadline}
	board := policyRulesBoardWithBudget(state, budget)
	if board == nil || len(board.Snakes) == 0 || len(board.Snakes) > 4 {
		return nil
	}
	search := survivalSearch{
		budget: budget, you: state.You.ID, solo: state.Game.Ruleset.Name == "solo" || len(board.Snakes) == 1,
		ruleset: rules.NewRulesetBuilder().WithSolo(true).WithParams(map[string]string{
			rules.ParamFoodSpawnChance: "0", rules.ParamMinimumFood: "0",
			rules.ParamHazardDamagePerTurn: strconv.Itoa(state.Game.Ruleset.Settings.HazardDamagePerTurn),
		}).NamedRuleset(rules.GameTypeStandard),
	}
	var completed map[string]bool
	for horizon := 2; horizon <= depth; horizon++ {
		current := make(map[string]bool, len(choices))
		for _, move := range choices {
			result := search.survive(board, horizon, move)
			if result < 0 || budget.stopped() {
				// Only compare moves at a horizon completed for every alternative.
				return completed
			}
			current[move] = result > 0
		}
		completed = current
	}
	return completed
}

type survivalSearch struct {
	budget  *moveBudget
	ruleset rules.Ruleset
	you     string
	solo    bool
	nodes   int
}

// Results are conditional on existing food; future random spawns are not predicted.
func (s *survivalSearch) survive(board *rules.BoardState, remaining int, forced string) int {
	if s.budget.stopped() || s.nodes >= 30000 {
		return -1
	}
	live := make([]rules.Snake, 0, len(board.Snakes))
	found := false
	for _, snake := range board.Snakes {
		if snake.EliminatedCause == rules.NotEliminated {
			live = append(live, snake)
			found = found || snake.ID == s.you
		}
	}
	if !found {
		return 0
	}
	if remaining == 0 || len(live) == 1 && !s.solo {
		return 1
	}
	current := *board
	current.Snakes = live
	unknown := false
	for _, direction := range directions {
		if forced != "" && direction.name != forced {
			continue
		}
		moves := make([]rules.SnakeMove, len(live))
		var replies func(int) int
		replies = func(index int) int {
			if s.budget.stopped() || s.nodes >= 30000 {
				return -1
			}
			if index == len(live) {
				s.nodes++
				_, next, err := s.ruleset.Execute(&current, moves)
				if err != nil {
					return -1
				}
				return s.survive(next, remaining-1, "")
			}
			moves[index].ID = live[index].ID
			if live[index].ID == s.you {
				moves[index].Move = direction.name
				return replies(index + 1)
			}
			unresolved := false
			for _, reply := range directions {
				moves[index].Move = reply.name
				result := replies(index + 1)
				if result == 0 {
					return 0
				}
				unresolved = unresolved || result < 0
			}
			if unresolved {
				return -1
			}
			return 1
		}
		result := replies(0)
		if result == 1 {
			return 1
		}
		unknown = unknown || result < 0
	}
	if unknown {
		return -1
	}
	return 0
}
