package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tmk-glance/internal/config"
	"tmk-glance/internal/observability"
	"tmk-glance/internal/server"
)

func main() {
	environment := strings.TrimSpace(os.Getenv("TMK_ENVIRONMENT"))
	if environment == "" {
		environment = "development"
	}
	observability.ConfigureLogging(environment)
	configPath := strings.TrimSpace(os.Getenv("TMK_CONFIG"))
	if configPath == "" {
		configPath = "config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	router := server.SetupRouter(cfg)
	srv := &http.Server{
		Addr:    cfg.Server.Port,
		Handler: router,
	}

	go func() {
		log.Printf("[startup] TMK-Glance server starting on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("[shutdown] shutting down server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %s\n", err)
	}
}
