package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestSurvivalAvoidsTournamentCorridor(t *testing.T) {
	data, err := os.ReadFile("testdata/tournament-survival.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Cases []struct {
			Request GameState `json:"request"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, position := range saved.Cases {
		policy := defaultPolicies()[5]
		if got := selectMove(position.Request, policy); got != "up" {
			t.Fatalf("published control chose %s, want recorded up", got)
		}
		encoded, _ := json.Marshal(policy)
		var config map[string]any
		if err := json.Unmarshal(encoded, &config); err != nil {
			t.Fatal(err)
		}
		config["survival_depth"] = 3
		encoded, _ = json.Marshal(config)
		if err := json.Unmarshal(encoded, &policy); err != nil {
			t.Fatal(err)
		}
		if got := selectMove(position.Request, policy); got != "left" && got != "down" {
			t.Fatalf("survival policy chose %s into the closing corridor; want left or down", got)
		}
		choices := []policyCandidate{{move: "up"}, {move: "down"}, {move: "left"}}
		safe := map[string]bool{"up": true, "down": true, "left": true}
		result := policySurvivalMoves(position.Request, choices, safe, newMoveBudget(context.Background(), time.Now(), 500), 3)
		if len(result) != 3 || result["up"] || !result["left"] || !result["down"] {
			t.Fatalf("wrong completed three-turn survival result: %v", result)
		}
		counted := &checkpointContext{Context: context.Background(), cancel: func() {}, remaining: 100000}
		shallow := policySurvivalMoves(position.Request, choices, safe, newMoveBudget(counted, time.Now(), 500), 2)
		if len(shallow) != 3 || !shallow["up"] || !shallow["left"] || !shallow["down"] {
			t.Fatalf("fixture should not distinguish the trap at two turns: %v", shallow)
		}
		for _, checks := range []int{2, 100000 - counted.remaining + 3} {
			ctx, cancel := context.WithCancel(context.Background())
			controlled := &checkpointContext{Context: ctx, cancel: cancel, remaining: checks}
			got := policySurvivalMoves(position.Request, choices, safe, newMoveBudget(controlled, time.Now(), 500), 3)
			cancel()
			if checks == 2 && got != nil {
				t.Fatalf("interrupted first horizon should have no result: %v", got)
			}
			if checks > 2 && (len(got) != 3 || !got["up"] || !got["left"] || !got["down"]) {
				t.Fatalf("interrupted deeper horizon should retain only the completed shallow result: %v", got)
			}
		}
	}
}
