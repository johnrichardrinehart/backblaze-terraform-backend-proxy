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

	s, err := backend.NewServer("localhost:8080", "", "")
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
