package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const moveRequest = `{
  "game":{"id":"local-test","ruleset":{"name":"standard","version":"v1.2.3","settings":{"hazardDamagePerTurn":14}},"timeout":500,"map":"standard","source":"testing"},
  "turn":1,
  "board":{"width":5,"height":5,"food":[],"hazards":[],"snakes":[{"id":"us","name":"Abraham Go","health":90,"head":{"x":0,"y":0},"length":3,"body":[{"x":0,"y":0},{"x":0,"y":1},{"x":1,"y":1}]}]},
  "you":{"id":"us","name":"Abraham Go","health":90,"head":{"x":0,"y":0},"length":3,"body":[{"x":0,"y":0},{"x":0,"y":1},{"x":1,"y":1}]},
  "future_api_field":"ignored"
}`

func TestHTTPLifecycle(t *testing.T) {
	handler := newHandler()
	for _, path := range []string{"/", "/start", "/move", "/end"} {
		method, body := http.MethodPost, moveRequest
		if path == "/" {
			method, body = http.MethodGet, ""
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if path == "/" && (got["apiversion"] != "1" || got["author"] != "mabraham23") {
			t.Fatalf("metadata = %v", got)
		}
		if path == "/move" && got["move"] != "right" {
			t.Fatalf("move response = %v, want right", got)
		}
	}
}

func TestHTTPRejectsInvalidMoveRequests(t *testing.T) {
	handler := newHandler()
	for _, body := range []string{"{", "null", "{}", moveRequest + "{}", strings.Replace(moveRequest, `"width":5`, `"width":0`, 1), strings.ReplaceAll(moveRequest, `"length":3`, `"length":0`)} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/move", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Errorf("invalid body returned %d: %s", response.Code, body)
		}
	}
}
