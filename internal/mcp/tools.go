package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// prop is a shorthand for building a JSON Schema property.
func prop(typ, desc string) any {
	return map[string]any{
		"type":        typ,
		"description": desc,
	}
}

func propEnum(typ, desc string, enum []string) any {
	vals := make([]any, len(enum))
	for i, v := range enum {
		vals[i] = v
	}
	return map[string]any{
		"type":        typ,
		"description": desc,
		"enum":        vals,
	}
}

// RegisterTools adds all ClaudeTalk tools to the MCP server.
func RegisterTools(srv *mcpserver.MCPServer, client *HTTPClient) {
	// 1. send_message
	srv.AddTool(mcplib.Tool{
		Name:        "send_message",
		Description: "Send a message to the chatroom. Omit `to` for a public message visible to everyone. Set `to` to send a private whisper visible only to that user and yourself.",
		InputSchema: mcplib.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"text": prop("string", "The message text to send"),
				"type": propEnum("string", "Message type: text (default), code, or diff", []string{"text", "code", "diff"}),
				"to":   prop("string", "Optional: recipient name for a private message visible only to them"),
			},
			Required: []string{"text"},
		},
	}, makeSendMessageHandler(client))

	// 2. converse
	srv.AddTool(mcplib.Tool{
		Name:        "converse",
		Description: "Send a direct conversation message to a specific participant. Use this for directed questions/replies.",
		InputSchema: mcplib.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"to":      prop("string", "Recipient name"),
				"message": prop("string", "The message text"),
				"conv_id": prop("string", "Conversation ID (auto-generated if omitted)"),
				"done":    prop("boolean", "Set true to mark conversation as complete (no reply expected)"),
			},
			Required: []string{"to", "message"},
		},
	}, makeConverseHandler(client))

	// 3. get_messages
	srv.AddTool(mcplib.Tool{
		Name:        "get_messages",
		Description: "Read recent messages from the chatroom.",
		InputSchema: mcplib.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"latest": prop("number", "Get the last N messages (default: 20)"),
				"after":  prop("number", "Get messages after this sequence number"),
			},
		},
	}, makeGetMessagesHandler(client))

	// 4. send_file
	srv.AddTool(mcplib.Tool{
		Name:        "send_file",
		Description: "Upload a file to share with other participants in the room.",
		InputSchema: mcplib.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"path":        prop("string", "Local file path to upload"),
				"description": prop("string", "Optional description of the file"),
			},
			Required: []string{"path"},
		},
	}, makeSendFileHandler(client))

	// 5. get_file
	srv.AddTool(mcplib.Tool{
		Name:        "get_file",
		Description: "Download a shared file from the room.",
		InputSchema: mcplib.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"file_id":   prop("string", "The file ID to download"),
				"save_path": prop("string", "Local path to save the file to"),
			},
			Required: []string{"file_id", "save_path"},
		},
	}, makeGetFileHandler(client))

	// 6. list_files
	srv.AddTool(mcplib.Tool{
		Name:        "list_files",
		Description: "List all shared files in the room.",
		InputSchema: mcplib.ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
	}, makeListFilesHandler(client))

	// 7. list_participants
	srv.AddTool(mcplib.Tool{
		Name:        "list_participants",
		Description: "List all participants connected to the room.",
		InputSchema: mcplib.ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
		},
	}, makeListParticipantsHandler(client))

}

func makeSendMessageHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		text := request.GetString("text", "")
		msgType := request.GetString("type", "text")
		to := request.GetString("to", "")
		if text == "" {
			return mcplib.NewToolResultError("text is required"), nil
		}

		var metadata map[string]string
		if to != "" {
			metadata = map[string]string{
				"to":      to,
				"private": "true",
			}
		}

		env, err := client.SendMessage(text, msgType, metadata)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to send: %v", err)), nil
		}

		if to != "" {
			return mcplib.NewToolResultText(fmt.Sprintf("Private message sent to %s (seq #%d)", to, env.SeqNum)), nil
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Message sent (seq #%d)", env.SeqNum)), nil
	}
}

func makeConverseHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		to := request.GetString("to", "")
		message := request.GetString("message", "")
		convID := request.GetString("conv_id", "")
		done := request.GetBool("done", false)

		if to == "" || message == "" {
			return mcplib.NewToolResultError("to and message are required"), nil
		}
		if convID == "" {
			convID = uuid.New().String()
		}

		expectingReply := "true"
		if done {
			expectingReply = "false"
		}

		metadata := map[string]string{
			"to":              to,
			"conv_id":         convID,
			"expecting_reply": expectingReply,
		}

		env, err := client.SendMessage(message, "text", metadata)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to send: %v", err)), nil
		}

		status := "sent"
		if done {
			status = "sent (conversation complete)"
		}
		return mcplib.NewToolResultText(fmt.Sprintf("Conversation message %s to %s (seq #%d, conv_id: %s)", status, to, env.SeqNum, convID)), nil
	}
}

func makeGetMessagesHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		latest := request.GetInt("latest", 20)
		after := int64(request.GetFloat("after", 0))
		if after > 0 {
			latest = 0
		}

		list, err := client.GetMessages(latest, after)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to get messages: %v", err)), nil
		}

		if len(list.Messages) == 0 {
			return mcplib.NewToolResultText("No messages found."), nil
		}

		var sb strings.Builder
		for _, env := range list.Messages {
			ts := env.Timestamp.Local().Format("15:04:05")
			fmt.Fprintf(&sb, "[#%d %s] %s", env.SeqNum, ts, env.Sender)
			if to := env.Metadata["to"]; to != "" {
				fmt.Fprintf(&sb, " → %s", to)
			}
			switch env.Type {
			case "text":
				fmt.Fprintf(&sb, ": %s", env.Payload.Text)
			case "code":
				fmt.Fprintf(&sb, " shared code:\n```%s\n%s\n```", env.Payload.Language, env.Payload.Code)
			case "diff":
				fmt.Fprintf(&sb, " shared diff:\n%s", env.Payload.Diff)
			case "file":
				fmt.Fprintf(&sb, ": %s", env.Payload.Text)
			case "system":
				fmt.Fprintf(&sb, " --- %s", env.Payload.Text)
			default:
				fmt.Fprintf(&sb, ": %s", env.Payload.Text)
			}
			if env.Metadata["expecting_reply"] == "true" {
				fmt.Fprintf(&sb, " (reply expected)")
			}
			if convID := env.Metadata["conv_id"]; convID != "" {
				short := convID
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(&sb, " conv:%s", short)
			}
			sb.WriteString("\n")
		}

		return mcplib.NewToolResultText(sb.String()), nil
	}
}

func makeSendFileHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		path := request.GetString("path", "")
		description := request.GetString("description", "")
		if path == "" {
			return mcplib.NewToolResultError("path is required"), nil
		}

		info, err := client.UploadFile(path, description)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to upload: %v", err)), nil
		}

		return mcplib.NewToolResultText(fmt.Sprintf("File uploaded: %s (id: %s, size: %d bytes)", info.Filename, info.ID, info.Size)), nil
	}
}

func makeGetFileHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		fileID := request.GetString("file_id", "")
		savePath := request.GetString("save_path", "")
		if fileID == "" || savePath == "" {
			return mcplib.NewToolResultError("file_id and save_path are required"), nil
		}

		if err := client.DownloadFile(fileID, savePath); err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to download: %v", err)), nil
		}

		return mcplib.NewToolResultText(fmt.Sprintf("File saved to: %s", savePath)), nil
	}
}

func makeListFilesHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		list, err := client.ListFiles()
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to list files: %v", err)), nil
		}

		if len(list.Files) == 0 {
			return mcplib.NewToolResultText("No files shared in this room."), nil
		}

		var sb strings.Builder
		for _, f := range list.Files {
			ts := f.Timestamp.Local().Format("15:04:05")
			fmt.Fprintf(&sb, "[%s] %s: %s (%d bytes) id:%s", ts, f.Sender, f.Filename, f.Size, f.ID)
			if f.Description != "" {
				fmt.Fprintf(&sb, " — %s", f.Description)
			}
			sb.WriteString("\n")
		}

		return mcplib.NewToolResultText(sb.String()), nil
	}
}

func makeListParticipantsHandler(client *HTTPClient) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, request mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		list, err := client.ListParticipants()
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("failed to list participants: %v", err)), nil
		}

		if len(list.Participants) == 0 {
			return mcplib.NewToolResultText("No participants in this room."), nil
		}

		var sb strings.Builder
		for _, p := range list.Participants {
			status := "disconnected"
			if p.Connected {
				status = "connected"
			}
			fmt.Fprintf(&sb, "%s (role: %s, %s, joined: %s)\n", p.Name, p.Role, status, p.JoinedAt.Local().Format("15:04:05"))
		}

		return mcplib.NewToolResultText(sb.String()), nil
	}
}
