package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/BattlesnakeOfficial/rules"
)

func TestSuccessorAvoidsSavedTraps(t *testing.T) {
	data, err := os.ReadFile("testdata/successor-traps.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Cases []struct {
			Purpose     string    `json:"purpose"`
			Observed    string    `json:"observed_move"`
			Alternative string    `json:"alternative_to_investigate"`
			Request     GameState `json:"request"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	// These positions have an exit closure or head contest within the candidate's horizon.
	for _, position := range saved.Cases {
		t.Run(position.Purpose, func(t *testing.T) {
			cautious := defaultPolicies()[2]
			if got := selectMove(position.Request, cautious); got != position.Observed {
				t.Fatalf("cautious changed: got %s, want recorded %s", got, position.Observed)
			}
			var successor Policy
			for _, policy := range defaultPolicies() {
				if policy.Name == "successor" {
					successor = policy
				}
			}
			if successor.Name == "" {
				t.Fatal("successor candidate is not selectable")
			}
			successor.SuccessorBudgetMS = 200
			if got := selectMove(position.Request, successor); got != position.Alternative {
				t.Errorf("successor chose %s, want available route %s", got, position.Alternative)
			}
		})
	}
	data, err = os.ReadFile("testdata/successor-mobility.json")
	if err != nil {
		t.Fatal(err)
	}
	var mobility []struct {
		Request     GameState `json:"request"`
		Observed    string    `json:"observed"`
		Alternative string    `json:"alternative"`
	}
	if err := json.Unmarshal(data, &mobility); err != nil {
		t.Fatal(err)
	}
	for _, position := range mobility {
		policy := defaultPolicies()[5]
		policy.SuccessorBudgetMS = 200
		if got := selectMove(position.Request, policy); got != position.Observed {
			t.Fatalf("published policy changed: got %s, want %s", got, position.Observed)
		}
		encoded, _ := json.Marshal(policy)
		var config map[string]any
		if err := json.Unmarshal(encoded, &config); err != nil {
			t.Fatal(err)
		}
		config["mobility_weight"] = 25
		encoded, _ = json.Marshal(config)
		if err := json.Unmarshal(encoded, &policy); err != nil {
			t.Fatal(err)
		}
		if got := selectMove(position.Request, policy); got != position.Alternative {
			t.Errorf("mobility policy chose %s, want the route with more safe continuations %s", got, position.Alternative)
		}
	}
}

func TestSuccessorTransitionAndCancellation(t *testing.T) {
	state := policyTestState([]Coord{{1, 1}, {1, 0}, {0, 0}}, 90)
	state.Board.Food = []Coord{{2, 1}}
	state.Board.Snakes = append(state.Board.Snakes, Battlesnake{ID: "other", Health: 1, Head: Coord{3, 1}, Body: []Coord{{3, 1}, {3, 0}, {4, 0}}, Length: 3})
	ruleset := rules.NewRulesetBuilder().WithSolo(true).NamedRuleset(rules.GameTypeStandard)
	_, next, err := ruleset.Execute(policyRulesBoard(state), []rules.SnakeMove{{ID: "us", Move: "right"}, {ID: "other", Move: "up"}})
	if err != nil {
		t.Fatal(err)
	}
	successor, alive := policySuccessorState(state, next, nil)
	if !alive || len(successor.Board.Snakes) != 1 || successor.You.Health != 100 || len(successor.You.Body) != 4 || successor.You.Body[3] != (Coord{1, 0}) || len(successor.Board.Food) != 0 {
		t.Fatalf("transition lost growth, consumption, or elimination state: %+v", successor)
	}
	if !policySafeMoves(successor)["right"] {
		t.Fatal("eliminated opponent body still blocks the next move")
	}
	state = successor
	candidates := []policyCandidate{{move: "right"}, {move: "up"}}
	safe := policySafeMoves(state)
	if !safe["right"] || !safe["up"] {
		t.Fatal("cancellation fixture requires two safe continuations")
	}
	for _, weight := range []float64{0, 25} {
		policy := Policy{SuccessorBudgetMS: 200, TrapPenalty: 1, MobilityWeight: weight}
		if got := policySuccessorPenalties(state, candidates, safe, newMoveBudget(context.Background(), time.Now(), 500), policy); len(got) != 2 {
			t.Fatalf("uninterrupted supplemental search did not complete: %v", got)
		}
		counted := &checkpointContext{Context: context.Background(), cancel: func() {}, remaining: 10000}
		if got := policySuccessorPenalties(state, candidates[:1], safe, newMoveBudget(counted, time.Now(), 500), policy); len(got) != 1 {
			t.Fatalf("first candidate did not complete: %v", got)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Cancel at the next checkpoint after the first candidate completes.
		controlled := &checkpointContext{Context: ctx, cancel: cancel, remaining: 10000 - counted.remaining}
		budget := newMoveBudget(controlled, time.Now(), 500)
		if got := policySuccessorPenalties(state, candidates, safe, budget, policy); got != nil || ctx.Err() != context.Canceled {
			t.Fatalf("interrupted supplemental search kept partial penalties: %v, context=%v", got, ctx.Err())
		}
		counted = &checkpointContext{Context: context.Background(), cancel: func() {}, remaining: 100000}
		baseline := policySuccessorPenalties(state, candidates, safe, newMoveBudget(counted, time.Now(), 500), policy)
		policy.EscapeDepth = 24
		routeCtx, routeCancel := context.WithCancel(context.Background())
		defer routeCancel()
		routeBudget := newMoveBudget(&checkpointContext{Context: routeCtx, cancel: routeCancel, remaining: 100000 - counted.remaining + 20}, time.Now(), 500)
		if got := policySuccessorPenalties(state, candidates, safe, routeBudget, policy); !reflect.DeepEqual(got, baseline) || routeCtx.Err() != context.Canceled {
			t.Fatalf("interrupted route search lost completed baseline penalties: got %v, want %v, context=%v", got, baseline, routeCtx.Err())
		}
	}
}
