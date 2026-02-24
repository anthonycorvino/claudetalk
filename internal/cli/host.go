package cli

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/corvino/claudetalk/internal/runner"
	"github.com/corvino/claudetalk/internal/server"
	"github.com/spf13/cobra"
)

func newHostCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "host",
		Short: "Start server and public tunnel — share the URL with friends",
		Long: `Starts the ClaudeTalk server locally and opens a public tunnel via localtunnel.
Share the printed URL with friends so they can run "claudetalk join <url>".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHost(port)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "local server port")
	return cmd
}

func runHost(port int) error {
	// 1. Start the embedded server.
	hub := server.NewHub(1000)
	addr := fmt.Sprintf(":%d", port)

	// Create file store in a temp directory for the host command.
	fileStore, err := server.NewFileStore(fmt.Sprintf("claudetalk-files-%d", port), 50*1024*1024)
	if err != nil {
		return fmt.Errorf("create file store: %w", err)
	}

	serverURL := fmt.Sprintf("http://localhost:%d", port)
	r := runner.New(runner.Config{
		ServerURL: serverURL,
	})

	srv := server.New(hub, addr, fileStore, r)

	go func() {
		log.Printf("Starting ClaudeTalk server on port %d...", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Give the server a moment to bind.
	time.Sleep(500 * time.Millisecond)

	// Quick health check.
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/health", port))
	if err != nil {
		return fmt.Errorf("server failed to start: %w", err)
	}
	resp.Body.Close()
	fmt.Println("Server is running.")

	// 2. Check that npx exists.
	npxPath, err := exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("npx not found — install Node.js from https://nodejs.org")
	}
	_ = npxPath

	// 3. Launch localtunnel.
	fmt.Println("Starting public tunnel...")

	ltCmd := exec.Command("npx", "localtunnel", "--port", fmt.Sprintf("%d", port))
	ltCmd.Stderr = os.Stderr

	stdout, err := ltCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe stdout: %w", err)
	}
	if err := ltCmd.Start(); err != nil {
		return fmt.Errorf("start localtunnel: %w", err)
	}

	// Read lines from localtunnel stdout until we find the URL.
	tunnelURL := ""
	scanner := bufio.NewScanner(stdout)
	urlCh := make(chan string, 1)

	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			// localtunnel prints: "your url is: https://xxx.loca.lt"
			if strings.Contains(line, "https://") {
				for _, word := range strings.Fields(line) {
					if strings.HasPrefix(word, "https://") {
						urlCh <- word
						return
					}
				}
			}
		}
		close(urlCh)
	}()

	// Wait up to 30s for the URL.
	select {
	case u, ok := <-urlCh:
		if !ok || u == "" {
			return fmt.Errorf("localtunnel exited without providing a URL")
		}
		tunnelURL = u
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timed out waiting for localtunnel URL")
	}

	// 4. Print the banner.
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println("  ClaudeTalk is LIVE!")
	fmt.Println()
	fmt.Println("  SHARE THIS URL WITH YOUR FRIENDS:")
	fmt.Println()
	fmt.Printf("  %s\n", tunnelURL)
	fmt.Println()
	fmt.Println("  They run:  claudetalk join " + tunnelURL)
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Printf("Local server:  http://localhost:%d\n", port)
	fmt.Printf("Public URL:    %s\n", tunnelURL)
	fmt.Println()
	fmt.Println("Press Ctrl+C to shut down.")
	fmt.Println()

	// 5. Wait for Ctrl+C, then graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("\nShutting down...")

	// Kill localtunnel.
	if ltCmd.Process != nil {
		ltCmd.Process.Kill()
	}

	// Shutdown HTTP server.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	fmt.Println("Stopped.")
	return nil
}
