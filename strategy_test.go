package main

import "testing"

func TestChooseMoveSurvival(t *testing.T) {
	tests := []struct {
		name      string
		body      []Coord
		health    int
		food      []Coord
		hazards   []Coord
		opponents []Battlesnake
		want      []string
	}{
		{name: "corner leaves only right", body: []Coord{{0, 0}, {0, 1}, {1, 1}}, health: 90, want: []string{"right"}},
		{name: "food saves last health", body: []Coord{{2, 2}, {2, 1}, {2, 0}}, health: 1, food: []Coord{{3, 2}}, want: []string{"right"}},
		{name: "food overrides hazard damage", body: []Coord{{2, 2}, {2, 1}, {2, 0}}, health: 1, food: []Coord{{3, 2}}, hazards: []Coord{{3, 2}}, want: []string{"right"}},
		{name: "avoid lethal hazard on food route", body: []Coord{{2, 2}, {2, 1}, {2, 0}}, health: 10, food: []Coord{{4, 2}}, hazards: []Coord{{3, 2}}, want: []string{"up", "left"}},
		{name: "trapped snake still returns a direction", body: []Coord{{0, 0}, {0, 1}, {1, 1}, {1, 0}, {1, 0}}, health: 90, want: []string{"up", "down", "left", "right"}},
		{name: "hungry snake follows reachable food", body: []Coord{{2, 2}, {2, 1}, {2, 0}}, health: 3, food: []Coord{{2, 4}}, want: []string{"up"}},
		{name: "unstacked own tail vacates", body: []Coord{{0, 0}, {0, 1}, {1, 1}, {1, 0}}, health: 90, want: []string{"right"}},
		{name: "stacked tail stays occupied", body: []Coord{{1, 1}, {1, 0}, {2, 0}, {2, 1}, {2, 1}}, health: 20, food: []Coord{{3, 1}}, want: []string{"up", "left"}},
		{name: "avoid equal length head contest", body: []Coord{{1, 2}, {1, 1}, {1, 0}}, health: 90, food: []Coord{{2, 2}}, opponents: []Battlesnake{{ID: "other", Health: 90, Head: Coord{3, 2}, Length: 3, Body: []Coord{{3, 2}, {3, 1}, {3, 0}}}}, want: []string{"up", "left"}},
		{name: "food in a one square pocket is a trap", body: []Coord{{2, 2}, {2, 1}, {3, 1}, {4, 1}, {4, 2}, {4, 3}, {3, 3}, {2, 3}, {1, 3}}, health: 50, food: []Coord{{3, 2}}, want: []string{"left"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			you := Battlesnake{ID: "us", Health: tt.health, Head: tt.body[0], Length: len(tt.body), Body: tt.body}
			state := GameState{You: you, Game: Game{Ruleset: Ruleset{Settings: RulesetSettings{HazardDamagePerTurn: 14}}}, Board: Board{Width: 5, Height: 5, Food: tt.food, Hazards: tt.hazards, Snakes: append([]Battlesnake{you}, tt.opponents...)}}
			got := chooseMove(state)
			for _, want := range tt.want {
				if got == want {
					return
				}
			}
			t.Fatalf("move = %q, want one of %v", got, tt.want)
		})
	}
}
