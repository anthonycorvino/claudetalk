package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/corvino/claudetalk/internal/runner"
	"github.com/corvino/claudetalk/internal/server"
)

func main() {
	port := flag.Int("port", 8080, "listen port")
	maxHistory := flag.Int("max-history", 1000, "max messages per room")
	fileDir := flag.String("file-dir", "claudetalk-files", "directory for file storage")
	maxFileSize := flag.Int64("max-file-size", 50*1024*1024, "max file size in bytes (default 50MB)")
	claudeBin := flag.String("claude-bin", "claude", "path to claude CLI binary")
	noClaude := flag.Bool("no-claude", false, "disable Claude spawning")
	flag.Parse()

	hub := server.NewHub(*maxHistory)

	fileStore, err := server.NewFileStore(*fileDir, *maxFileSize)
	if err != nil {
		log.Fatalf("create file store: %v", err)
	}

	addr := fmt.Sprintf(":%d", *port)
	serverURL := fmt.Sprintf("http://localhost:%d", *port)

	var r *runner.Runner
	if !*noClaude {
		r = runner.New(runner.Config{
			ClaudeBin: *claudeBin,
			ServerURL: serverURL,
		})
		log.Println("Claude runner enabled (local subprocess)")
	} else {
		log.Println("Claude runner disabled")
	}

	srv := server.New(hub, addr, fileStore, r)

	// Graceful shutdown on SIGINT/SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("claudetalk-server listening on %s", addr)
		log.Printf("open http://localhost:%d in your browser", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-stop
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("server stopped")
}
