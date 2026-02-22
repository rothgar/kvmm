package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
)

//go:embed static
var staticFiles embed.FS

func main() {
	if len(os.Args) < 2 {
		// No args = list devices
		runList()
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "server", "serve":
		runServer()
	case "list", "ls":
		runList()
	case "help", "-h", "--help":
		printCLIUsage()
	default:
		// Treat as device alias/host lookup
		query := strings.Join(os.Args[1:], " ")
		runOpen(query)
	}
}

func runServer() {
	// Parse server-specific flags
	serverFlags := flag.NewFlagSet("server", flag.ExitOnError)
	configPath := serverFlags.String("config", "config.toml", "Path to configuration file")
	portOverride := serverFlags.Int("port", 0, "Override port from config")
	serverFlags.Parse(os.Args[2:])

	// Load configuration
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override port if specified
	if *portOverride > 0 {
		cfg.Server.Port = *portOverride
	}

	// Create handlers
	handlers := NewHandlers(cfg)

	// Setup routes
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/devices", handlers.DevicesHandler)
	mux.HandleFunc("/api/devices/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/thumbnail") {
			handlers.ThumbnailHandler(w, r)
			return
		}
		handlers.DevicesHandler(w, r)
	})

	// Thumbnail serving route
	mux.HandleFunc("/thumbnails/", handlers.ServeThumbnail)

	// Device status route
	mux.HandleFunc("/api/status", handlers.CheckDevicesStatus)

	// KVM redirect route
	mux.HandleFunc("/go/", handlers.GoToDevice)

	// Static files (embedded)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to setup static files: %v", err)
	}

	// Serve index.html for root path
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			data, err := staticFiles.ReadFile("static/index.html")
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		http.ServeFileFS(w, r, staticFS, path)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Printf("KVMM server starting on http://localhost%s", addr)
	log.Printf("Using config file: %s", *configPath)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
