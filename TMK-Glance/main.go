package main

import (
	"context"
	"log"
	"net/http"
	"net/http/pprof"
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
	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/debug/pprof/", pprof.Index)
	debugMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	debugMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	debugMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	debugMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	debugMux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	debugMux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	debugMux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	debugMux.Handle("/debug/pprof/block", pprof.Handler("block"))
	debugMux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	debugAddr := strings.TrimSpace(os.Getenv("PPROF_ADDR"))
	if debugAddr == "" {
		debugAddr = "127.0.0.1:6060"
	}
	debugSrv := &http.Server{Addr: debugAddr, Handler: debugMux}
	go func() {
		if err := debugSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[pprof] server stopped: %v", err)
		}
	}()

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
	_ = debugSrv.Shutdown(ctx)
}
