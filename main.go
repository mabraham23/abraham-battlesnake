package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := arenaCommand(ctx, os.Args[1:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	address := net.JoinHostPort(os.Getenv("HOST"), port)
	policy := Policy{Name: "baseline", Kind: "baseline"}
	if filename := os.Getenv("SNAKE_CONFIG"); filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			log.Fatal(err)
		}
		if err := validatePolicy(policy); err != nil {
			log.Fatal(err)
		}
		if policy.URL != "" {
			log.Fatal("SNAKE_CONFIG must select a local policy")
		}
	}
	server := &http.Server{
		Addr: address, Handler: handlerWithPolicy(policy),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Abraham Go listening on %s", address)
	log.Fatal(server.ListenAndServe())
}
