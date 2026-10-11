package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"email-client/pkg/config"
	"email-client/pkg/logger"
	"email-client/pkg/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.json", "path to config file")
	addr := flag.String("addr", "127.0.0.1:8081", "listen address (keep it on localhost: the API has no login)")
	webDir := flag.String("web", "", "folder with the built web UI to serve at /")
	debug := flag.Bool("debug", false, "write SMTP/POP3 protocol lines to the log")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log, closer, err := logger.Open(cfg.LogPath, level)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer closer.Close()

	srv := &http.Server{
		Handler:           server.New(cfg, log, *webDir),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	log.Info("HTTP API listening", "addr", *addr)
	fmt.Printf("API on http://%s  (Ctrl-C to stop, log: %s)\n", *addr, cfg.LogPath)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("HTTP API stopped")
	return nil
}
