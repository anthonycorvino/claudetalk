package daemon

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Config holds daemon configuration.
type Config struct {
	ServerURL     string
	Room          string
	Name          string
	ClaudeBin     string
	WorkDir       string
	MaxConcurrent int
}

// Run starts the daemon event loop. Blocks until interrupted.
func Run(cfg Config) error {
	if cfg.WorkDir == "" {
		var err error
		cfg.WorkDir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	ws := NewWSConn(cfg.ServerURL, cfg.Room, cfg.Name)
	spawner := NewSpawner(cfg.ClaudeBin, cfg.WorkDir, cfg.ServerURL, cfg.Room, cfg.Name, cfg.MaxConcurrent)

	// Handle signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Start WebSocket connection in background.
	go ws.Run()

	log.Printf("daemon started: room=%s name=%s", cfg.Room, cfg.Name)
	log.Printf("waiting for events...")

	for {
		select {
		case event := <-ws.Events():
			switch event.Event {
			case "spawn":
				if event.Spawn != nil {
					log.Printf("spawn event: reason=%s", event.Spawn.Reason)
					go func() {
						if err := spawner.Spawn(event.Spawn); err != nil {
							log.Printf("spawn error: %v", err)
						}
					}()
				}
			case "message":
				if event.Message != nil {
					log.Printf("message: [#%d] %s: %s",
						event.Message.SeqNum,
						event.Message.Sender,
						truncate(event.Message.Payload.Text, 80))
				}
			case "file_shared":
				if event.File != nil {
					log.Printf("file shared: %s by %s (%d bytes)",
						event.File.Filename,
						event.File.Sender,
						event.File.Size)
				}
			default:
				log.Printf("unknown event: %s", event.Event)
			}

		case <-sigCh:
			log.Println("shutting down daemon...")
			ws.Close()
			return nil
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
