package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestBodyRouteGrowthAndHealth(t *testing.T) {
	ring := []Coord{{0, 0}, {0, 1}, {1, 1}, {1, 0}}
	for _, test := range []struct {
		name          string
		body          []Coord
		health, depth int
		food          bool
		want          int
	}{
		{"vacating tail keeps route open", ring, 90, 24, false, 24},
		{"growth closes the route", ring, 1, 24, true, 1},
		{"food cannot be eaten twice", ring[:3], 1, 104, true, 100},
		{"starvation ends route", ring, 3, 24, false, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			search := routeSearch{board: Board{Width: 2, Height: 2}, food: map[Coord]bool{}, hazards: map[Coord]int{}, blocked: map[Coord]bool{}}
			if test.food {
				search.food[Coord{1, 0}] = true
			}
			before, _ := json.Marshal(test.body)
			if got := search.route(test.body, test.health, test.depth, "right"); got != test.want {
				t.Fatalf("route survives %d turns, want %d", got, test.want)
			}
			after, _ := json.Marshal(test.body)
			if string(before) != string(after) || search.food[Coord{1, 0}] != test.food {
				t.Fatal("route search failed to restore its input")
			}
			search.nodes = 4096
			if got := search.route(test.body, test.health, test.depth, "right"); got != -1 {
				t.Fatalf("exhausted search reported a completed result: %d", got)
			}
			search.nodes = 0
			search.budget = newMoveBudget(context.Background(), time.Now().Add(-time.Second), 500)
			if got := search.route(test.body, test.health, test.depth, "right"); got != -1 {
				t.Fatalf("interrupted search reported a completed result: %d", got)
			}
		})
	}
}
