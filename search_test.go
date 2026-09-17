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
		duel     int
		doomed   string
		without  string
		with     []string
		searched int
	}{
		{"corridor closes beyond the flood fill", corridor, 5, 0, "left", "left", []string{"up"}, 2},
		{"duel depth extends a shallow search", corridor, 2, 5, "left", "left", []string{"up"}, 2},
		{"stacked opening with five snakes and no food", opening, 3, 0, "", "", []string{"up", "down", "left"}, 3},
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
			policy.SearchDepth, policy.SearchDepthDuel, policy.SearchLengthWeight = tt.depth, tt.duel, 120
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

func TestSearchDuelOptions(t *testing.T) {
	state := policyTestState([]Coord{{5, 0}, {5, 1}, {5, 2}}, 100)
	state.Board.Width, state.Board.Height = 11, 11
	state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: "b", Health: 100, Head: Coord{5, 9}, Length: 3, Body: []Coord{{5, 9}, {5, 8}, {5, 7}}})
	budget := newMoveBudget(context.Background(), time.Now(), 500)
	board := policyRulesBoardWithBudget(state, budget)
	plain := newParanoidSearch(state, board, budget, Policy{SearchDepth: 4})
	duel := newParanoidSearch(state, board, budget, Policy{SearchDepth: 4, SearchBudgetDuelMS: 200, SearchEdgePenalty: 40})
	var out [4]string
	plain.markOccupied(board.Snakes)
	duel.markOccupied(board.Snakes)
	if got := plain.opponentMoves(board.Snakes[1], board.Snakes[0].Body[0], 2, &out); got != 1 {
		t.Errorf("distant opponent without duel branching gave %d moves, want 1", got)
	}
	if got := duel.opponentMoves(board.Snakes[1], board.Snakes[0].Body[0], 2, &out); got != 3 {
		t.Errorf("distant opponent with duel branching gave %d moves, want 3", got)
	}
	if edge, free := duel.evaluate(board, board.Snakes, 0, 0), plain.evaluate(board, board.Snakes, 0, 0); edge >= free {
		t.Errorf("edge penalty did not lower an edge head: %.0f vs %.0f", edge, free)
	}
}

func TestSearchChokePenalty(t *testing.T) {
	// A 7x7 board: we sit in the two-row bottom strip and the opponent's body walls off row two,
	// leaving a two-cell frontier at x=4 that its head can close in a few moves.
	strip := policyTestState([]Coord{{2, 1}, {1, 1}, {0, 1}, {0, 0}, {1, 0}}, 100)
	strip.Board.Width, strip.Board.Height = 7, 7
	strip.Board.Snakes = append(strip.Board.Snakes, Battlesnake{ID: "b", Health: 100, Head: Coord{5, 2}, Length: 17,
		Body: []Coord{{5, 2}, {4, 2}, {3, 2}, {2, 2}, {1, 2}, {0, 2}, {0, 3}, {1, 3}, {2, 3}, {3, 3}, {4, 3}, {4, 4}, {3, 4}, {2, 4}, {1, 4}, {0, 4}, {0, 5}}})
	open := policyTestState([]Coord{{5, 5}, {5, 4}, {5, 3}}, 100)
	open.Board.Width, open.Board.Height = 11, 11
	open.Board.Snakes = append(open.Board.Snakes, Battlesnake{ID: "b", Health: 100, Head: Coord{5, 9}, Length: 3, Body: []Coord{{5, 9}, {5, 8}, {5, 7}}})
	budget := newMoveBudget(context.Background(), time.Now(), 500)
	for name, tt := range map[string]struct {
		state     GameState
		penalised bool
	}{"closable strip": {strip, true}, "open board": {open, false}} {
		board := policyRulesBoardWithBudget(tt.state, budget)
		plain := newParanoidSearch(tt.state, board, budget, Policy{SearchDepth: 4})
		choke := newParanoidSearch(tt.state, board, budget, Policy{SearchDepth: 4, SearchChokeWeight: 1500})
		with, without := choke.evaluate(board, board.Snakes, 0, 0), plain.evaluate(board, board.Snakes, 0, 0)
		if (with < without) != tt.penalised {
			t.Errorf("%s: choke %.0f vs plain %.0f, want penalised=%v", name, with, without, tt.penalised)
		}
	}
}
