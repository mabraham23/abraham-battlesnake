package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMoveRequestDeadline(t *testing.T) {
	for _, tt := range []struct {
		name      string
		timeout   int
		cancelled bool
		delay     time.Duration
		want      string
	}{
		{"ample budget", 500, false, 0, "right"},
		{"cancelled request", 500, true, 0, "up"},
		{"response margin consumes tiny budget", 1, false, 0, "up"},
		{"decode time counts against budget", 10, false, 15 * time.Millisecond, "up"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := policyTestState([]Coord{{2, 2}, {2, 1}, {2, 0}}, 90)
			state.Game.ID, state.Game.Timeout = "deadline", tt.timeout
			state.Board.Food = []Coord{{3, 2}}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range defaultPolicies() {
				ctx, cancel := context.WithCancel(context.Background())
				if tt.cancelled {
					cancel()
				}
				body := &delayedMoveBody{Reader: strings.NewReader(string(encoded)), delay: tt.delay}
				request := httptest.NewRequest(http.MethodPost, "/move", body).WithContext(ctx)
				response := httptest.NewRecorder()
				handlerWithPolicy(p).ServeHTTP(response, request)
				cancel()
				var got BattlesnakeMoveResponse
				if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &got) != nil || got.Move != tt.want {
					t.Errorf("%s: response=%d %s, want %s", p.Name, response.Code, response.Body.String(), tt.want)
				}
			}
		})
	}
}

func TestMoveDeadlineDecision(t *testing.T) {
	state := policyTestState([]Coord{{2, 2}, {2, 1}, {2, 0}}, 90)
	state.Board.Food = []Coord{{3, 2}}
	for _, tt := range []struct {
		timeout int
		age     time.Duration
		want    string
	}{
		{10, 20 * time.Millisecond, "up"},
		{100, 120 * time.Millisecond, "up"},
		{500, 120 * time.Millisecond, "right"},
		{2000, 600 * time.Millisecond, "right"},
		{0, 0, "right"},
		{-1, 0, "right"},
	} {
		state.Game.Timeout = tt.timeout
		for _, p := range defaultPolicies() {
			move, err := safePolicyContext(context.Background(), time.Now().Add(-tt.age), state, p)
			if err != nil || move != tt.want {
				t.Errorf("%s timeout=%d age=%s: got %s, %v; want %s", p.Name, tt.timeout, tt.age, move, err, tt.want)
			}
		}
	}
	for _, tt := range []struct {
		name string
		body []Coord
		food []Coord
		want string
	}{
		{"wall and body", []Coord{{0, 0}, {0, 1}, {1, 1}}, nil, "right"},
		{"vacating tail", []Coord{{0, 0}, {0, 1}, {1, 1}, {1, 0}}, nil, "right"},
		{"food saves last health in hazard", []Coord{{2, 2}, {2, 1}, {2, 0}}, []Coord{{3, 2}}, "right"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := policyTestState(tt.body, 90)
			state.Board.Food = tt.food
			if tt.food != nil {
				state.You.Health = 1
				state.Board.Hazards = tt.food
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			for _, p := range defaultPolicies() {
				if got := selectMoveContext(ctx, time.Now(), state, p); got != tt.want {
					t.Errorf("%s fallback=%s, want %s", p.Name, got, tt.want)
				}
			}
		})
	}
	t.Run("parent deadline bounds search", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if got := selectMoveContext(ctx, time.Now(), state, defaultPolicies()[2]); got != "up" {
			t.Fatalf("expired parent returned %s, want fallback up", got)
		}
	})
	t.Run("cancel during traversal discards incomplete results", func(t *testing.T) {
		board := Board{Width: 100, Height: 100}
		for _, search := range []func(*moveBudget){
			func(b *moveBudget) {
				if got := policyDistancesWithBudget(Coord{0, 0}, board, nil, nil, nil, nil, b); got != nil {
					t.Errorf("cancelled distances returned %d partial cells", len(got))
				}
			},
			func(b *moveBudget) {
				if space, food := exploreWithBudget(Coord{0, 0}, board, nil, nil, nil, b); space != 0 || food != -1 {
					t.Errorf("cancelled exploration returned %d space, %d food distance", space, food)
				}
			},
		} {
			ctx, cancel := context.WithCancel(context.Background())
			controlled := &checkpointContext{Context: ctx, cancel: cancel, remaining: 1}
			search(newMoveBudget(controlled, time.Now(), 500))
			if ctx.Err() != context.Canceled {
				t.Error("search never observed cancellation after traversal began")
			}
			cancel()
		}
	})
}

type checkpointContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *checkpointContext) Err() error {
	if c.remaining == 0 {
		c.cancel()
	} else {
		c.remaining--
	}
	return c.Context.Err()
}

type delayedMoveBody struct {
	io.Reader
	delay time.Duration
}

func (b *delayedMoveBody) Read(p []byte) (int, error) {
	if b.delay > 0 {
		time.Sleep(b.delay)
		b.delay = 0
	}
	return b.Reader.Read(p)
}
