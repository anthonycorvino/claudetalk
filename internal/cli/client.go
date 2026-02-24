package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func apiURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func postMessage(server, room string, req protocol.SendRequest) (*protocol.Envelope, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := apiURL(server, fmt.Sprintf("/api/rooms/%s/messages", room))
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var env protocol.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &env, nil
}

func getMessages(server, room string, after int64, limit int) (*protocol.MessageList, error) {
	url := apiURL(server, fmt.Sprintf("/api/rooms/%s/messages?after=%d&limit=%d", room, after, limit))
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var list protocol.MessageList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &list, nil
}

func getLatestMessages(server, room string, n int) (*protocol.MessageList, error) {
	url := apiURL(server, fmt.Sprintf("/api/rooms/%s/messages/latest?n=%d", room, n))
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var list protocol.MessageList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &list, nil
}

func getRooms(server string) (*protocol.RoomList, error) {
	url := apiURL(server, "/api/rooms")
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	var list protocol.RoomList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &list, nil
}

func getHealth(server string) (*protocol.HealthResponse, error) {
	url := apiURL(server, "/api/health")
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	var health protocol.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &health, nil
}

// formatPlain formats an envelope for human-readable output.
func formatPlain(env protocol.Envelope) string {
	var b strings.Builder
	ts := env.Timestamp.Local().Format("15:04:05")
	fmt.Fprintf(&b, "[#%d %s] %s", env.SeqNum, ts, env.Sender)

	// Show conversation metadata if present.
	if to := env.Metadata["to"]; to != "" {
		fmt.Fprintf(&b, " → %s", to)
	}

	switch env.Type {
	case protocol.TypeText:
		fmt.Fprintf(&b, ": %s", env.Payload.Text)
	case protocol.TypeCode:
		fmt.Fprintf(&b, " shared code")
		if env.Payload.FilePath != "" {
			fmt.Fprintf(&b, " (%s)", env.Payload.FilePath)
		}
		if env.Payload.Language != "" {
			fmt.Fprintf(&b, " [%s]", env.Payload.Language)
		}
		fmt.Fprintf(&b, ":\n```%s\n%s\n```", env.Payload.Language, env.Payload.Code)
	case protocol.TypeDiff:
		fmt.Fprintf(&b, " shared diff")
		if env.Payload.FilePath != "" {
			fmt.Fprintf(&b, " (%s)", env.Payload.FilePath)
		}
		fmt.Fprintf(&b, ":\n%s", env.Payload.Diff)
	case protocol.TypeSystem:
		fmt.Fprintf(&b, " --- %s", env.Payload.Text)
	default:
		fmt.Fprintf(&b, ": %s", env.Payload.Text)
	}

	// Show conversation indicators.
	if env.Metadata["expecting_reply"] == "true" {
		fmt.Fprintf(&b, " (reply expected)")
	} else if env.Metadata["expecting_reply"] == "false" {
		fmt.Fprintf(&b, " (conversation complete)")
	}
	if convID := env.Metadata["conv_id"]; convID != "" {
		fmt.Fprintf(&b, " conv:%s", convID[:8])
	}

	return b.String()
}

// ANSI color codes for sender coloring.
var senderColors = []string{
	"\033[36m", // Cyan
	"\033[32m", // Green
	"\033[33m", // Yellow
	"\033[35m", // Magenta
	"\033[34m", // Blue
	"\033[31m", // Red
	"\033[96m", // Bright Cyan
	"\033[92m", // Bright Green
}

const ansiReset = "\033[0m"

// senderColor returns a deterministic ANSI color for a sender name.
func senderColor(name string) string {
	var h uint32
	for _, c := range name {
		h = h*31 + uint32(c)
	}
	return senderColors[h%uint32(len(senderColors))]
}

// formatColor wraps formatPlain with ANSI color on the sender name.
func formatColor(env protocol.Envelope) string {
	plain := formatPlain(env)
	color := senderColor(env.Sender)
	// Replace first occurrence of sender name with colored version.
	colored := strings.Replace(plain, "] "+env.Sender, "] "+color+env.Sender+ansiReset, 1)
	return colored
}
