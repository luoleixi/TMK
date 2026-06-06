package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"

	"tmk-glance/internal/config"
	"tmk-glance/internal/server"
)

func main() {
	cfg, err := config.Load("config.yaml")
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
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("[shutdown] shutting down server ...")

	if err := srv.Close(); err != nil {
		log.Fatalf("shutdown: %s\n", err)
	}
}
