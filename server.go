package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"time"
)

func newHandler() http.Handler {
	return handlerWithPolicy(Policy{Name: "baseline", Kind: "baseline"})
}

func handlerWithPolicy(policy Policy) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, BattlesnakeInfoResponse{
			APIVersion: "1", Author: "mabraham23", Color: envOr("SNAKE_COLOR", "#B91C1C"), Head: envOr("SNAKE_HEAD", "evil"), Tail: envOr("SNAKE_TAIL", "sharp"),
		})
	})
	debug := os.Getenv("DEBUG_MOVES") == "1"
	for _, path := range []string{"/start", "/move", "/end"} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			var state GameState
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
			if err := decoder.Decode(&state); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid game JSON"})
				return
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) || state.Game.ID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected one game state with a game ID"})
				return
			}
			if path == "/move" {
				if !validMoveState(state) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid board or snake"})
					return
				}
				move := selectMoveContext(r.Context(), started, state, policy)
				if debug {
					log.Printf("game=%s turn=%d health=%d length=%d move=%s elapsed=%s", state.Game.ID, state.Turn, state.You.Health, state.You.Length, move, time.Since(started))
				}
				writeJSON(w, http.StatusOK, BattlesnakeMoveResponse{Move: move})
				return
			}
			log.Printf("game=%s event=%s turn=%d", state.Game.ID, path, state.Turn)
			writeJSON(w, http.StatusOK, struct{}{})
		})
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("response: %v", err)
	}
}
func validMoveState(state GameState) bool {
	b, you := state.Board, state.You
	if b.Width < 1 || b.Height < 1 || b.Width > 100 || b.Height > 100 ||
		you.ID == "" || you.Health < 1 || you.Health > 100 || len(you.Body) == 0 ||
		you.Head != you.Body[0] || you.Length != len(you.Body) || !inside(you.Head, b) {
		return false
	}
	if !slices.ContainsFunc(b.Snakes, func(s Battlesnake) bool { return s.ID == you.ID }) {
		return false
	}
	for _, s := range b.Snakes {
		if len(s.Body) == 0 || s.Head != s.Body[0] || s.Length != len(s.Body) {
			return false
		}
		for _, p := range s.Body {
			if !inside(p, b) {
				return false
			}
		}
	}
	for _, p := range you.Body {
		if !inside(p, b) {
			return false
		}
	}
	return true
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
