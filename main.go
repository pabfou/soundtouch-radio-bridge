package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"soundtouch-radio-bridge/internal/api"
	"soundtouch-radio-bridge/internal/config"
	"soundtouch-radio-bridge/internal/speaker"
	"soundtouch-radio-bridge/internal/tunein"
)

// localIPFor returns the IP the OS would use to reach target. Uses a UDP
// "connection" which never sends a packet but populates the local address.
func localIPFor(target string) string {
	conn, err := net.Dial("udp", target+":1")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

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
		if ip := localIPFor(cfg.Speakers[0].IP); ip != "" {
			// addr is like ":8080" — keep the port portion
			port := strings.TrimPrefix(*addr, ":")
			bridgeURL := "http://" + ip + ":" + port
			mgr.SetBridgeURL(bridgeURL)
			log.Printf("bridge URL for stream proxy: %s", bridgeURL)
		}
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
