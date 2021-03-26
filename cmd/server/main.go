package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	backend "github.com/johnrichardrinehart/backblaze-terraform-backend-proxy/pkg"
)

func main() {
	keyID, exists := os.LookupEnv("B2_KEY_ID")
	if !exists {
		log.Fatal("B2_KEY_ID env var is undefined")
	}

	appKey, exists := os.LookupEnv("B2_APP_KEY")
	if !exists {
		log.Fatal("B2_APP_KEY env var is undefined")
	}

	b2, err := backend.NewB2(keyID, appKey, "test")
	if err != nil {
		log.Fatalf("failed to construct B2 backend: %s", err)
	}
	log.Println("store connection established successfully")

	s, err := backend.NewServer("localhost:8080", b2)
	if err != nil {
		log.Fatalf("failed to start server: %s", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.Shutdown(ctx)
		cancel()
	}()

	if err := s.Start(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("failed to start server: %s", err)
	}
}
