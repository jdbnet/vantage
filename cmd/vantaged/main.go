package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

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

	log.Printf("vantaged %s listening on %s (data %s)", appcore.Version, core.Listen, *dataDir)
	srv := &http.Server{Addr: core.Listen, Handler: core.Handler()}
	if secure {
		log.Fatal(srv.ListenAndServeTLS(*tlsCert, *tlsKey))
	}
	log.Fatal(srv.ListenAndServe())
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
