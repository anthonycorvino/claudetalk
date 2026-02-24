package mcp

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// Config holds the configuration for the MCP server.
type Config struct {
	ServerURL string
	Room      string
	Name      string
}

// Serve starts the MCP stdio server. It blocks until stdin is closed or a signal is received.
func Serve(cfg Config) error {
	client := NewHTTPClient(cfg.ServerURL, cfg.Room, cfg.Name)

	srv := mcpserver.NewMCPServer(
		"claudetalk",
		"2.0.0",
		mcpserver.WithToolCapabilities(true),
	)

	RegisterTools(srv, client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	stdioSrv := mcpserver.NewStdioServer(srv)
	return stdioSrv.Listen(ctx, os.Stdin, os.Stdout)
}
