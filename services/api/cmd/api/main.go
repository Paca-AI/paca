// Command api is the entry point for the Paca API service.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/paca/api/internal/bootstrap"
	"github.com/paca/api/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	app, err := bootstrap.New(cfg)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	// Run server in a goroutine so we can listen for shutdown signals.
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Run()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	case sig := <-quit:
		log.Printf("received signal %s — shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		shutdownErr := app.Shutdown(ctx)
		cancel()
		if shutdownErr != nil {
			log.Fatalf("shutdown error: %v", shutdownErr)
		}
	}
}
