package main

import (
	"context"
	"testing"
	"time"
)

func TestSearchValuesAndSelection(t *testing.T) {
	stacked := func(id string, x, y int) Battlesnake {
		return Battlesnake{ID: id, Health: 100, Head: Coord{x, y}, Body: []Coord{{x, y}, {x, y}, {x, y}}, Length: 3}
	}
	// A hungry snake is lured by food into a one-wide corridor that closes five turns later.
	corridor := policyTestState([]Coord{{4, 0}, {5, 0}, {6, 0}, {6, 1}}, 30)
	corridor.Board.Width, corridor.Board.Height = 7, 7
	corridor.Board.Food = []Coord{{3, 0}}
	corridor.Board.Snakes = append(corridor.Board.Snakes, Battlesnake{ID: "b", Health: 90, Head: Coord{0, 2}, Length: 9,
		Body: []Coord{{0, 2}, {0, 1}, {1, 1}, {2, 1}, {3, 1}, {3, 2}, {3, 3}, {3, 4}, {3, 5}}})
	opening := policyTestState([]Coord{{1, 1}, {1, 1}, {1, 1}}, 100)
	opening.Board.Width, opening.Board.Height = 11, 11
	opening.Board.Snakes = append(opening.Board.Snakes, stacked("b", 3, 1), stacked("c", 9, 1), stacked("d", 1, 9), stacked("e", 9, 9))
	base := Policy{Name: "search", Kind: "heuristic", SpaceWeight: 10, FoodWeight: 5000, HungerThreshold: 45, TerritoryWeight: 3, TailWeight: 50, HeadRisk: 20000, TrapPenalty: 3000, Lookahead: true, SearchBudgetMS: 300}
	for _, tt := range []struct {
		name     string
		state    GameState
		depth    int
		doomed   string
		without  string
		with     []string
		searched int
	}{
		{"corridor closes beyond the flood fill", corridor, 5, "left", "left", []string{"up"}, 2},
		{"stacked opening with five snakes and no food", opening, 3, "", "", []string{"up", "down", "left"}, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validatePolicy(base); err != nil {
				t.Fatal(err)
			}
			safe := policySafeMoves(tt.state)
			candidates := make([]policyCandidate, 0, 4)
			for move := range safe {
				candidates = append(candidates, policyCandidate{move: move})
			}
			policy := base
			policy.SearchDepth = tt.depth
			values := policySearchValues(tt.state, candidates, safe, newMoveBudget(context.Background(), time.Now(), 500), policy)
			if len(values) != tt.searched {
				t.Fatalf("searched %d moves %v, want %d", len(values), values, tt.searched)
			}
			for move, value := range values {
				if doomed := move == tt.doomed; doomed != (value < -1e5) {
					t.Errorf("%s valued %.0f, doomed=%v", move, value, doomed)
				}
			}
			if tt.without != "" {
				if got := selectMove(tt.state, base); got != tt.without {
					t.Fatalf("without search chose %s, want %s", got, tt.without)
				}
			}
			got := selectMove(tt.state, policy)
			for _, want := range tt.with {
				if got == want {
					return
				}
			}
			t.Errorf("with search chose %s, want one of %v", got, tt.with)
		})
	}
}
