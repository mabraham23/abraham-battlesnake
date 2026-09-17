package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"testing"
)

func TestPolicyPreservesTerritoryDifference(t *testing.T) {
	data, err := os.ReadFile("testdata/territory-confinement.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Request GameState `json:"request"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	policy := defaultPolicies()[5]
	if got := selectMove(saved.Request, policy); got != "down" {
		t.Fatalf("published control chose %s, want recorded down", got)
	}
	encoded, _ := json.Marshal(policy)
	var config map[string]any
	json.Unmarshal(encoded, &config)
	config["uncapped_territory"] = true
	encoded, _ = json.Marshal(config)
	json.Unmarshal(encoded, &policy)
	if got := selectMove(saved.Request, policy); got != "up" {
		t.Fatalf("territory-aware policy chose %s, want up toward more controlled room", got)
	}
}

func TestSelectMoveCandidateSurvival(t *testing.T) {
	tests := []struct {
		name          string
		body          []Coord
		health        int
		food, hazards []Coord
		want          []string
	}{
		{"only vacating tail is open", []Coord{{0, 0}, {0, 1}, {1, 1}, {1, 0}}, 90, nil, nil, []string{"right"}},
		{"duplicated tail remains occupied", []Coord{{1, 1}, {1, 0}, {2, 0}, {2, 1}, {2, 1}}, 20, []Coord{{3, 1}}, nil, []string{"up", "left"}},
		{"food saves last health in hazard", []Coord{{2, 2}, {2, 1}, {2, 0}}, 1, []Coord{{3, 2}}, []Coord{{3, 2}}, []string{"right"}},
		{"hungry snake takes reachable food path", []Coord{{2, 2}, {2, 1}, {2, 0}}, 3, []Coord{{2, 4}}, nil, []string{"up"}},
		{"food pocket cannot reach moving tail", []Coord{{2, 2}, {2, 1}, {3, 1}, {4, 1}, {4, 2}, {4, 3}, {3, 3}, {2, 3}, {1, 3}}, 50, []Coord{{3, 2}}, nil, []string{"left"}},
		{"trapped snake returns a direction", []Coord{{0, 0}, {0, 1}, {1, 1}, {1, 0}, {1, 0}}, 90, nil, nil, []string{"up", "right", "down", "left"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := policyTestState(tt.body, tt.health)
			state.Board.Food, state.Board.Hazards = tt.food, tt.hazards
			before, _ := json.Marshal(state)
			for _, p := range defaultPolicies() {
				if p.Kind == "baseline" {
					if got := selectMove(state, p); got != chooseMove(state) {
						t.Fatalf("baseline changed: %s", got)
					}
					continue
				}
				got := selectMove(state, p)
				if !slices.Contains(tt.want, got) {
					t.Errorf("%s move = %s, want %v", p.Name, got, tt.want)
				}
				if again := selectMove(state, p); again != got {
					t.Errorf("%s is nondeterministic", p.Name)
				}
			}
			after, _ := json.Marshal(state)
			if string(before) != string(after) {
				t.Fatal("policy mutated caller's game state")
			}
		})
	}
	t.Run("opponent tail is the only surviving move", func(t *testing.T) {
		state := policyTestState([]Coord{{0, 0}, {0, 1}, {1, 1}}, 90)
		state.Board.Food = []Coord{{3, 1}}
		state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: "other", Health: 90, Body: []Coord{{3, 0}, {2, 0}, {1, 0}}, Head: Coord{3, 0}, Length: 3})
		for _, p := range defaultPolicies() {
			if p.Kind == "heuristic" {
				if got := selectMove(state, p); got != "right" {
					t.Errorf("%s move = %s, want right into vacating opponent tail", p.Name, got)
				}
			}
		}
	})
	t.Run("tail preference depends on the candidate", func(t *testing.T) {
		state := policyTestState([]Coord{{2, 2}, {2, 1}, {3, 1}, {4, 1}, {4, 2}, {4, 3}, {3, 3}, {2, 3}, {1, 3}}, 50)
		state.Board.Food = []Coord{{3, 2}}
		p := Policy{Name: "tail-only", Kind: "heuristic", TailWeight: 100}
		if got := selectMove(state, p); got != "left" {
			t.Fatalf("move = %s, want left toward reachable tail", got)
		}
	})
}

func TestPolicyOneTurnOfficialOutcomes(t *testing.T) {
	tests := []struct {
		name            string
		body, otherBody []Coord
		otherHealth     int
		food            []Coord
		want            bool
	}{
		{"equal length contest loses", []Coord{{1, 2}, {1, 1}, {1, 0}}, []Coord{{3, 2}, {3, 1}, {3, 0}}, 90, []Coord{{2, 2}}, false},
		{"longer snake wins shared food contest", []Coord{{1, 2}, {1, 1}, {1, 0}, {0, 0}}, []Coord{{3, 2}, {3, 1}, {3, 0}}, 90, []Coord{{2, 2}}, true},
		{"starving enemy cannot contest empty square", []Coord{{1, 2}, {1, 1}, {1, 0}}, []Coord{{3, 2}, {3, 1}, {3, 0}}, 1, nil, true},
		{"food saves starving enemy before contest", []Coord{{1, 2}, {1, 1}, {1, 0}}, []Coord{{3, 2}, {3, 1}, {3, 0}}, 1, []Coord{{2, 2}}, false},
		{"longer snake still dies entering old head", []Coord{{1, 2}, {1, 1}, {1, 0}, {0, 0}}, []Coord{{2, 2}, {2, 3}, {3, 3}}, 90, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := policyTestState(tt.body, 90)
			state.Board.Food = tt.food
			state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: "other", Health: tt.otherHealth, Body: tt.otherBody, Head: tt.otherBody[0], Length: len(tt.otherBody)})
			if got := policySafeMoves(state)["right"]; got != tt.want {
				t.Errorf("right survives every reply = %v, want %v", got, tt.want)
			}
			state.Board.Width, state.Board.Height = 11, 11
			for i, head := range []Coord{{8, 8}, {10, 8}, {8, 10}} {
				state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: fmt.Sprintf("distant-%d", i), Health: 90, Head: head, Body: []Coord{head, head, head}, Length: 3})
			}
			if got := policySafeMoves(state)["right"]; got != tt.want {
				t.Errorf("five-snake right survives every reply = %v, want %v", got, tt.want)
			}
		})
	}
	state := policyTestState([]Coord{{1, 2}, {1, 1}, {1, 0}}, 90)
	state.Board.Food = []Coord{{2, 2}}
	state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: "other", Health: 90, Body: []Coord{{3, 2}, {3, 1}, {3, 0}}, Head: Coord{3, 2}, Length: 3})
	p := Policy{Name: "food-only", Kind: "heuristic", HungerThreshold: 40, FoodWeight: 100, Lookahead: true}
	if got := selectMove(state, p); got == "right" {
		t.Fatal("lookahead failed to exclude losing food contest")
	}
	ours := map[Coord]int{{2, 2}: 0, {2, 3}: 1}
	other := []policyOpponent{{length: 3, distances: map[Coord]int{{2, 2}: 1, {2, 3}: 2}}}
	for _, tt := range []struct {
		length int
		ate    bool
		want   int
	}{{3, false, 0}, {4, true, 0}, {5, true, 2}} {
		if got := policyTerritory(ours, other, Coord{2, 2}, tt.length, tt.ate); got != tt.want {
			t.Errorf("territory at length %d, ate=%v: got %d, want %d", tt.length, tt.ate, got, tt.want)
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	for _, p := range defaultPolicies() {
		if err := validatePolicy(p); err != nil {
			t.Fatalf("preset %s: %v", p.Name, err)
		}
	}
	valid := Policy{Name: "custom", Kind: "heuristic", HungerThreshold: 40}
	for _, change := range []func(*Policy){
		func(p *Policy) { p.Name = "" },
		func(p *Policy) { p.Kind = "unknown" },
		func(p *Policy) { p.SpaceWeight = -1 },
		func(p *Policy) { p.FoodWeight = math.Inf(1) },
		func(p *Policy) { p.TailWeight = math.NaN() },
		func(p *Policy) { p.HungerThreshold = 101 },
		func(p *Policy) { p.TrapPenalty = 100001 },
		func(p *Policy) { p.SuccessorBudgetMS = -1 },
		func(p *Policy) { p.SuccessorBudgetMS = 201 },
		func(p *Policy) { p.SurvivalDepth = -1 },
		func(p *Policy) { p.SurvivalDepth = 1 },
		func(p *Policy) { p.SurvivalDepth = 6 },
		func(p *Policy) { p.EscapeDepth = 24 },
		func(p *Policy) { p.SuccessorLookahead = true; p.EscapeDepth = 7 },
		func(p *Policy) { p.SuccessorLookahead = true; p.EscapeDepth = 33 },
		func(p *Policy) { p.SearchDepth = 1 },
		func(p *Policy) { p.SearchDepth = 7 },
		func(p *Policy) { p.SearchBudgetMS = 301 },
		func(p *Policy) { p.SearchWeight = -1 },
	} {
		p := valid
		change(&p)
		if err := validatePolicy(p); err == nil {
			t.Errorf("accepted invalid policy %+v", p)
		}
	}
}

func BenchmarkSelectMovePolicies(b *testing.B) {
	state := policyTestState([]Coord{{1, 1}, {1, 1}, {1, 1}}, 100)
	state.Board.Width, state.Board.Height = 11, 11
	state.Board.Food = []Coord{{5, 5}, {1, 3}, {7, 9}}
	for i, head := range []Coord{{1, 9}, {9, 1}, {9, 9}} {
		state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: string(rune('a' + i)), Health: 100, Head: head, Body: []Coord{head, head, head}, Length: 3})
	}
	for _, p := range defaultPolicies() {
		b.Run(p.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				selectMove(state, p)
			}
		})
	}
}

func policyTestState(body []Coord, health int) GameState {
	you := Battlesnake{ID: "us", Health: health, Body: body, Head: body[0], Length: len(body)}
	return GameState{You: you, Board: Board{Width: 5, Height: 5, Snakes: []Battlesnake{you}}, Game: Game{Ruleset: Ruleset{Name: "standard", Settings: RulesetSettings{HazardDamagePerTurn: 14}}}}
}
