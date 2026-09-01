package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jdbnet/vantage/internal/appcore"
)

func main() {
	var (
		dataDir = flag.String("data-dir", defaultDataDir(), "data directory")
		listen  = flag.String("listen", "", "listen address (overrides settings)")
		tlsCert = flag.String("tls-cert", "", "optional TLS certificate")
		tlsKey  = flag.String("tls-key", "", "optional TLS key")
	)
	flag.Parse()

	secure := *tlsCert != "" && *tlsKey != ""
	core, err := appcore.Open(*dataDir, "daemon", *listen, secure)
	if err != nil {
		log.Fatal(err)
	}
	defer core.Close()

	tls := "off"
	if secure {
		tls = "on"
	}
	log.Printf("vantaged %s listening on %s (data %s tls=%s)", appcore.Version, core.Listen, *dataDir, tls)
	srv := &http.Server{
		Addr:              core.Listen,
		Handler:           core.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.Default(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("vantaged shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("vantaged shutdown: %v", err)
		}
	}()

	var serveErr error
	if secure {
		serveErr = srv.ListenAndServeTLS(*tlsCert, *tlsKey)
	} else {
		serveErr = srv.ListenAndServe()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatal(serveErr)
	}
	log.Printf("vantaged stopped")
}

func defaultDataDir() string {
	if d := os.Getenv("VANTAGE_DATA_DIR"); d != "" {
		return d
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "vantaged")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./data"
	}
	return filepath.Join(home, ".local", "share", "vantaged")
}
