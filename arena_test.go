package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestArenaConcurrentHTTPMoves(t *testing.T) {
	for _, mode := range []string{"success", "faults", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			arrivals := make(chan GameState, 4)
			finished := make(chan struct{}, 4)
			release := make(chan struct{})
			var overlap atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/move" {
					_, _ = fmt.Fprint(w, `{}`)
					return
				}
				defer func() { finished <- struct{}{} }()
				var state GameState
				if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
					t.Error(err)
					return
				}
				arrivals <- state
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if mode == "cancel" {
					<-r.Context().Done()
					return
				}
				if mode == "faults" {
					switch state.You.ID {
					case "seat-0":
						_, _ = fmt.Fprint(w, `{"move":"diagonal"}`)
						return
					case "seat-1":
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					case "seat-2":
						<-r.Context().Done()
						return
					}
				}
				_, _ = fmt.Fprint(w, `{"move":"up"}`)
			}))
			defer server.Close()
			go func() {
				for range 4 {
					select {
					case state := <-arrivals:
						if state.Turn != 0 || len(state.Board.Snakes) != 4 {
							t.Errorf("request did not use the initial shared board: %+v", state)
						}
						overlap.Add(1)
					case <-ctx.Done():
						return
					}
				}
				close(release)
				if mode == "cancel" {
					cancel()
				}
			}()
			settings := ArenaSettings{Width: 11, Height: 11, Ruleset: "standard", Map: "standard", TimeoutMS: 500, MaxTurns: 1}
			if mode == "cancel" {
				settings.MaxTurns = 2
			}
			p := Policy{Name: "http", Kind: "baseline", URL: server.URL}
			job := ArenaJob{ID: "concurrency", Seed: 42, Snakes: []Policy{p, p, p, p}, Trace: true}
			input, _ := json.Marshal(map[string]any{"settings": settings, "workers": 1, "games": []ArenaJob{job}, "concurrent_moves": true})
			var output bytes.Buffer
			err := runArena(ctx, bytes.NewReader(input), &output)
			if (mode == "cancel" && err != context.Canceled) || (mode != "cancel" && err != nil) {
				t.Fatalf("run error: %v", err)
			}
			var result ArenaResult
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if overlap.Load() != 4 || len(result.Decisions) != 4 {
				t.Fatalf("move requests did not overlap: arrivals=%d, decisions=%d", overlap.Load(), len(result.Decisions))
			}
			for seat, decision := range result.Decisions {
				if decision.Seat != seat || decision.Turn != 0 || decision.Move == "" {
					t.Fatalf("result ordering or fallback changed: %+v", decision)
				}
				if mode == "success" && (decision.ResponseMove != "up" || decision.Error != "" || decision.Timeout) {
					t.Fatalf("unexpected successful response: %+v", decision)
				}
			}
			if mode == "faults" {
				if result.Snakes[0].Errors != 1 || result.Snakes[1].Errors != 1 || result.Snakes[2].Timeouts != 1 || result.Snakes[3].Errors+result.Snakes[3].Timeouts != 0 {
					t.Fatalf("faults were attributed to the wrong seats: %+v", result.Snakes)
				}
			}
			if mode == "cancel" && result.Error != context.Canceled.Error() {
				t.Fatalf("cancellation was not retained: %+v", result)
			}
			for range 4 {
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("move handler remained active after the turn")
				}
			}
		})
	}
}

func TestArenaDeterminismAndTruncation(t *testing.T) {
	settings := ArenaSettings{Width: 11, Height: 11, Ruleset: "standard", Map: "standard", TimeoutMS: 500, MaxTurns: 2, FoodSpawnChance: 15, MinimumFood: 1}
	p := Policy{Name: "baseline", Kind: "baseline"}
	job := ArenaJob{ID: "fixture", Seed: 42, Snakes: []Policy{p, p, p, p}, Trace: true}
	first := simulateGame(context.Background(), settings, job, false)
	second := simulateGame(context.Background(), settings, job, false)
	if first.Error != "" || second.Error != "" {
		t.Fatalf("simulation errors: %s / %s", first.Error, second.Error)
	}
	if !first.Truncated || first.Winner != -1 {
		t.Fatalf("turn cap must not manufacture a win: %+v", first)
	}
	a, _ := json.Marshal(first.Frames)
	b, _ := json.Marshal(second.Frames)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("seeded frame sequence changed")
	}
	encoded, _ := json.Marshal(first.Frames[0].Game.Ruleset.Settings)
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"royale", "squad"} {
		if len(metadata[key]) == 0 || metadata[key][0] != '{' {
			t.Fatalf("standard requests must include %s settings for API compatibility", key)
		}
	}
	job.Seed = 0
	if got := simulateGame(context.Background(), settings, job, false); got.Error == "" {
		t.Fatal("seed zero silently permits global RNG")
	}
}

func TestArenaHTTPMatchesInProcess(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			server := httptest.NewServer(newHandler())
			defer server.Close()
			settings := ArenaSettings{Width: 11, Height: 11, Ruleset: "standard", Map: "standard", TimeoutMS: 500, MaxTurns: 50, FoodSpawnChance: 15, MinimumFood: 1}
			p := Policy{Name: "baseline", Kind: "baseline"}
			job := ArenaJob{ID: "parity", Seed: 123, Snakes: []Policy{p, p, p, p}, Trace: true}
			local := simulateGame(context.Background(), settings, job, false)
			for seat := range job.Snakes {
				job.Snakes[seat].URL = server.URL
			}
			remote := simulateGame(context.Background(), settings, job, concurrent)
			if local.Error != "" || remote.Error != "" {
				t.Fatalf("%s / %s", local.Error, remote.Error)
			}
			if !reflect.DeepEqual(local.Frames, remote.Frames) {
				t.Fatal("HTTP and direct policies produce different games")
			}
			for _, metrics := range remote.Snakes {
				if metrics.Errors != 0 || metrics.Timeouts != 0 {
					t.Fatalf("unexpected fault: %+v", metrics)
				}
			}
			var recorded [4]int
			for _, decision := range remote.Decisions {
				if decision.Turn < 0 || decision.Turn >= remote.Turns || decision.Seat < 0 || decision.Seat >= len(recorded) || decision.ElapsedMS < 0 || decision.Timeout || decision.Error != "" || decision.Move == "" || decision.ResponseMove != decision.Move {
					t.Fatalf("invalid decision trace: %+v", decision)
				}
				recorded[decision.Seat]++
			}
			for seat, metrics := range remote.Snakes {
				if recorded[seat] != metrics.Moves {
					t.Fatalf("seat %d trace has %d moves, want %d", seat, recorded[seat], metrics.Moves)
				}
			}
			bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
			defer bad.Close()
			job.Snakes[0].URL = bad.URL
			broken := simulateGame(context.Background(), settings, job, concurrent)
			if broken.Snakes[0].Errors == 0 {
				t.Fatal("failed HTTP opponent was silently accepted")
			}
			if len(broken.Decisions) == 0 || broken.Decisions[0].Error == "" || broken.Decisions[0].Move == "" || broken.Decisions[0].ResponseMove != "" {
				t.Fatal("failed response and applied fallback were not retained")
			}
			job.Trace = false
			if quiet := simulateGame(context.Background(), settings, job, concurrent); len(quiet.Decisions) != 0 {
				t.Fatal("non-trace games retained per-turn diagnostics")
			}
			slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/move" {
					time.Sleep(25 * time.Millisecond)
					return
				}
				_, _ = fmt.Fprint(w, `{}`)
			}))
			defer slow.Close()
			job.Trace, job.Snakes[0].URL = true, slow.URL
			settings.TimeoutMS, settings.MaxTurns = 10, 1
			timed := simulateGame(context.Background(), settings, job, concurrent)
			first := timed.Decisions[0]
			if timed.Snakes[0].Timeouts+timed.Snakes[0].Errors != 1 || first.Timeout != (timed.Snakes[0].Timeouts == 1) || first.Error == "" || first.ResponseMove != "" || first.Move == "" {
				t.Fatalf("timeout response and fallback differ from aggregate: %+v / %+v", first, timed.Snakes[0])
			}
		})
	}
}

func TestCallSnakeRequiresCompleteBoundedResponse(t *testing.T) {
	const limit = 1 << 20
	valid := `{"move":"right"}`
	for _, tt := range []struct {
		name, event, body, wantMove string
		wantError                   bool
	}{
		{"valid move", "move", valid, "right", false},
		{"trailing garbage", "move", valid + " garbage", "", true},
		{"multiple objects", "move", valid + valid, "", true},
		{"exact size limit", "move", valid + strings.Repeat(" ", limit-len(valid)), "right", false},
		{"oversized move", "move", valid + strings.Repeat(" ", limit+1-len(valid)), "", true},
		{"empty lifecycle response", "end", "", "", false},
		{"oversized lifecycle response", "end", strings.Repeat(" ", limit+1), "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			move, err := callSnake(context.Background(), server.Client(), server.URL, tt.event, GameState{})
			if (err != nil) != tt.wantError || move != tt.wantMove {
				t.Fatalf("move=%q, err=%v; want move=%q, error=%v", move, err, tt.wantMove, tt.wantError)
			}
		})
	}
}

func TestArenaParallelJobsRetainSeededBoards(t *testing.T) {
	p := Policy{Name: "baseline", Kind: "baseline"}
	settings := ArenaSettings{Width: 11, Height: 11, Ruleset: "standard", Map: "standard", TimeoutMS: 500, MaxTurns: 10, FoodSpawnChance: 15, MinimumFood: 1}
	batch := ArenaBatch{Settings: settings, Workers: 2}
	for i := range 6 {
		batch.Games = append(batch.Games, ArenaJob{ID: fmt.Sprint(i), Seed: 42, Snakes: []Policy{p, p, p, p}, Trace: true})
	}
	input, _ := json.Marshal(batch)
	var output bytes.Buffer
	if err := runArena(context.Background(), bytes.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var boards []Board
	count := 0
	for decoder.More() {
		var r ArenaResult
		if err := decoder.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		current := []Board{}
		for _, frame := range r.Frames {
			current = append(current, frame.Board)
		}
		if boards == nil {
			boards = current
		} else if !reflect.DeepEqual(boards, current) {
			t.Fatal("parallel scheduling changed seeded boards")
		}
		count++
	}
	if count != 6 {
		t.Fatalf("results=%d, want6", count)
	}
}
