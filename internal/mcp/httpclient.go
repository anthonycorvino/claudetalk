package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/corvino/claudetalk/internal/protocol"
)

// HTTPClient talks to the ClaudeTalk central server REST API.
type HTTPClient struct {
	BaseURL string
	Room    string
	Sender  string
	client  *http.Client
}

// NewHTTPClient creates a new HTTP client for the MCP tools.
func NewHTTPClient(baseURL, room, sender string) *HTTPClient {
	return &HTTPClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Room:    room,
		Sender:  sender,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *HTTPClient) url(path string) string {
	return c.BaseURL + path
}

// SendMessage posts a message to the room.
func (c *HTTPClient) SendMessage(text, msgType string, metadata map[string]string) (*protocol.Envelope, error) {
	if msgType == "" {
		msgType = protocol.TypeText
	}
	req := protocol.SendRequest{
		Sender:   c.Sender,
		Type:     msgType,
		Payload:  protocol.NewTextPayload(text),
		Metadata: metadata,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	resp, err := c.client.Post(c.url(fmt.Sprintf("/api/rooms/%s/messages", c.Room)), "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var env protocol.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &env, nil
}

// GetMessages fetches messages from the room.
func (c *HTTPClient) GetMessages(latest int, after int64) (*protocol.MessageList, error) {
	var u string
	if latest > 0 {
		u = c.url(fmt.Sprintf("/api/rooms/%s/messages/latest?n=%d", c.Room, latest))
	} else {
		u = c.url(fmt.Sprintf("/api/rooms/%s/messages?after=%d&limit=100", c.Room, after))
	}

	resp, err := c.client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var list protocol.MessageList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &list, nil
}

// UploadFile uploads a file to the room.
func (c *HTTPClient) UploadFile(filePath, description string) (*protocol.FileInfo, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, fmt.Errorf("copy file: %w", err)
	}

	w.WriteField("sender", c.Sender)
	if description != "" {
		w.WriteField("description", description)
	}
	w.Close()

	resp, err := c.client.Post(c.url(fmt.Sprintf("/api/rooms/%s/files", c.Room)), w.FormDataContentType(), &buf)
	if err != nil {
		return nil, fmt.Errorf("POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var info protocol.FileInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &info, nil
}

// DownloadFile downloads a file from the room and saves it to savePath.
func (c *HTTPClient) DownloadFile(fileID, savePath string) error {
	resp, err := c.client.Get(c.url(fmt.Sprintf("/api/rooms/%s/files/%s", c.Room, fileID)))
	if err != nil {
		return fmt.Errorf("GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	out, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("save file: %w", err)
	}
	return nil
}

// ListFiles lists all files in the room.
func (c *HTTPClient) ListFiles() (*protocol.FileList, error) {
	resp, err := c.client.Get(c.url(fmt.Sprintf("/api/rooms/%s/files", c.Room)))
	if err != nil {
		return nil, fmt.Errorf("GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var list protocol.FileList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &list, nil
}

// ListParticipants lists all participants in the room.
func (c *HTTPClient) ListParticipants() (*protocol.ParticipantList, error) {
	resp, err := c.client.Get(c.url(fmt.Sprintf("/api/rooms/%s/participants", c.Room)))
	if err != nil {
		return nil, fmt.Errorf("GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(b))
	}

	var list protocol.ParticipantList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &list, nil
}
