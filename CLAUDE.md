# ClaudeTalk — Multi-Agent Communication

This project uses **ClaudeTalk** for real-time communication between Claude Code instances.
Check for messages regularly and respond when appropriate.

## Connection (auto-configured)

The .claudetalk config file in this directory sets:
- Server: http://localhost:8080
- Room: myroom
- Sender: rabble

All commands below auto-read this config — no flags needed.

## Commands

Send a message:
```
claudetalk send "your message here"
```

Send code:
```
claudetalk send -t code -f path/to/file.py
```

Send a diff:
```
git diff | claudetalk send -t diff
```

Receive messages:
```
claudetalk recv --latest 5
```

## Conversations (Direct Claude-to-Claude)

To ask another Claude a direct question:
```
claudetalk converse --to <recipient-name> "your question here"
```
This returns a conversation ID. When replying to a directed conversation:
```
claudetalk converse --to <sender-name> --conv <conv-id> "your answer"
```
To end a conversation (no further reply expected):
```
claudetalk converse --to <sender-name> --conv <conv-id> --done "final answer"
```

**When you see a message directed to you** (with `→ your-name` and `(reply expected)`):
1. Read the question carefully
2. Respond using `converse --to <sender> --conv <id>`
3. Use `--done` if your response completes the conversation

## Chatroom (Group Discussion)

When multiple Claudes are in the same room, use `claudetalk send` for group discussion.
All participants see all messages. This is for collaborative topics where everyone should chime in.

**Group discussion guidelines:**
- Stay on topic — respond only when you have something relevant to contribute
- Be concise — everyone has limited context
- Announce what you're working on so others don't duplicate effort
- When asked a group question, respond with your perspective

## Auto-Polling (MANDATORY)

**IMPORTANT: At the START of every response, run `claudetalk poll`. If there are new messages, read and respond to them before continuing with the user's request.**

The poll command is silent when there are no new messages, so it won't clutter your output. It automatically tracks which messages you've already seen.

## When to Check Messages

- **Every single response** — run `claudetalk poll` (this is mandatory, not optional)
- **After completing work** — send a summary of what you did
- **When stuck** — ask via claudetalk before guessing
- **Before modifying shared files** — check if someone else is working on them

## Guidelines

1. Be concise — other agents have limited context too
2. Use `-t code` for code, `-t diff` for diffs — not plain text walls
3. Announce what you're working on before starting
4. Send the actual diff when done: `git diff | claudetalk send -t diff`
