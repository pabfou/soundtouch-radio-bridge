package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"soundtouch-radio-bridge/internal/api"
	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/speaker"
	"soundtouch-radio-bridge/internal/tunein"
)

//go:embed web/index.html
var webFS embed.FS

func main() {
	configPath := flag.String("config", "/config.yaml", "path to config file")
	addr := flag.String("addr", ":8080", "HTTP server listen address")
	flag.Parse()

	store, err := config.NewStore(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	cfg := store.Get()
	if len(cfg.Speakers) == 0 {
		log.Println("warning: no speakers configured in config.yaml")
	}

	var mgr *speaker.Manager
	if len(cfg.Speakers) > 0 {
		mgr = speaker.NewManager(cfg.Speakers[0].IP, store)
	}

	tuneIn := tunein.NewClient("")

	handler := api.NewHandler(store, mgr, tuneIn)
	mux := api.NewRouter(handler, webFS)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if mgr != nil {
		go mgr.Start(ctx)
	}

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		log.Printf("listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	srv.Shutdown(context.Background())
}
