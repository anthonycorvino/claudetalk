# ClaudeTalk Session Insights — 2026-02-24

## Room: myroom
## Participants: rabble, liam

---

## Liam's Polymarket Bot (Rust)

### Architecture
- Rust binary on AWS Lightsail (eu-west-1, Ireland), systemd service
- SQLite for trade history and state
- 4 parallel strategies (A/B/C/D) with independent risk managers
- Dashboard on port 3030 with websocket streaming
- Autonomous iterator: spawns fresh Claude Code agent every 15 min to tune Strategy C

### Data Feeds
- Binance perpetual futures (100ms kline), Binance spot, Coinbase, Kraken — all WebSocket
- Polymarket CLOB orderbook polled every 250ms
- **250ms price discovery edge**: Binance perp moves before Polymarket book updates

### Strategy C — Rebate-First Market Maker (Key Details)
- **Fair value model**: Brownian motion — P(Up) = Phi(d / (sigma * sqrt(t/300)))
  - sigma_5min = 0.003 (annualized 45% vol scaled to 5min via sqrt(105120))
  - Normal CDF via Abramowitz & Stegun formula 26.2.17 (max error 7.5e-8)
  - Uses perp-to-perp displacement, NOT perp-to-chainlink (eliminates structural basis)
- **Blended FV (iter50-51)**: p_up = 0.6 * coordinator_fv + 0.4 * (market_mid + brownian_delta)
- **Dual-sided maker quoting**: simultaneous YES and NO bids, book-anchored with displacement offset
- **Fee zones**: Quartic model — fee = 0.25 * (p * (1-p))^2
  - Green (>0.90/<0.10): taker viable
  - Amber (0.85-0.90/0.10-0.15): taker only with extreme edge
  - Red (0.15-0.85): maker only
- **Key parameters (iter55)**:
  - half_spread_base: 0.025
  - max_unpaired: 8 shares
  - total_position_cap: 24
  - bid_size: 8 shares
  - order_cooldown: 3s
  - taker_min_edge: 5c
  - max_bid_premium: 3c over fair
  - abs_bid_ceiling: 72c
- **EMA signal suppression**: alpha=0.05 (~20 tick half-life), filters coordinator_fv noise spikes
- **Pair cost guard**: YES_bid + NO_bid >= 0.98 prevents locked-in losses

### Strategy C Iteration History (Notable Bugs & Fixes)
- **Iter 8**: Runaway requoting bug — CancelAll didn't stop requoting, cancel->cooldown->requote loop
- **Iter 21**: Division by $0.01 ask → 1500-share FOK order, 201k exposure events in 14s (+$508 by luck)
- **Iter 44**: place_dual_quotes() was IGNORING its p_up parameter (named _p_up). All iters 2-43 were tuning a function that didn't use its own input
- **Iter 45-46**: Inverted suppression logic — suppressing the correct side instead of the wrong side
- **Iter 48**: Raised max_unpaired to 16, data showed worse results, reverted to 8 in iter52
- **Iter 49**: Found pair cost > 1.0 was a silent killer (YES@0.62 + NO@0.44 = 1.06)
- **Iter 50-51**: Plugged in coordinator signal — everything before this was blind market making
- **Iter 53**: Lowered cooldown 5s→3s for more rebate opportunity
- **Iter 54**: Added EMA innovation to smooth coordinator_fv oscillations
- **Iter 55**: EMA-aware pair-priority caps. PnL: +$0.40 across 14 windows. Approaching breakeven.

### Other Strategies
- **Strategy A**: +$29.93 net PnL, 16 resolved windows, 50% win rate, 5,352 trades
- **Strategy B**: 48,773 trades, 17 windows, 10W/7L
- **Strategy D**: Whale wallet following, 179 tracked wallets, launched same day. Some wallets showed 55-68% historical win rates. Main challenge: 2-10s attribution delay on wallet data.

### Performance Summary
- 54,848 total trades in database
- Strategy B strongest in paper trading
- Strategy C slightly negative but converging after 55 iterations

---

## Insights & Ideas Generated

### For Liam's Bot
- **Asymmetric bid sizing in EMA amber zone**: Instead of binary 8/0 (full/suppressed), use a gradient:
  - EMA < 0.40: suppress YES entirely (current)
  - EMA 0.40-0.50: 4 YES / 8 NO
  - EMA 0.50: 8/8 neutral
  - EMA 0.50-0.60: 8 YES / 4 NO
  - EMA > 0.60: suppress NO entirely
  - Reduces max adverse exposure from 8 to 4 on weak side during ambiguous signals
  - Liam flagged this for next iterator run

### For MoneyPrinter
- Consider Brownian motion fair value model instead of/alongside indicator-based scoring
- Perp-to-perp displacement instead of perp-to-reference price to avoid structural basis
- EMA smoothing on signals to prevent noise spike fills
- Pair cost awareness if ever adding maker strategy

### Architectural Comparison
- **Liam**: Microstructure-first (maker rebates, spread capture, inventory management). Makes money on the spread regardless of direction. Needs volume and thin books. Scales better in mature markets.
- **MoneyPrinter**: Signal-first (predict direction, bet on it, manage risk). Makes money on being right about direction. Needs predictive accuracy and timing. Works better in volatile markets.

### ClaudeTalk Feedback from Liam
- Use central server with WebSocket push to clients instead of polling
- Each participant should run a client that spawns Claude Code with context pushed automatically
- Server should handle turn-taking, message history, and system prompt injection for facilitation

---

## League of Legends Advice
- **Liam's picks for climbing out of Gold**: Ambessa top, Viego jungle, Garen top (braindead easy)
- Viego recommended for learning the game while climbing (pathing, skirmishing, teamfighting)

---

## ClaudeTalk Web UI — Session 2026-02-24 (Night)

### What We Built

Converted ClaudeTalk from CLI-only to a **web app with local Claude spawning**.

**Architecture:**
- **Fly.io (`claudetalk.fly.dev`)** = shared chat server (rooms, messages, WebSocket, files only)
- **Each user** runs `claudetalk web --server https://claudetalk.fly.dev` locally
- Opens `http://localhost:3000` — embedded dark-themed web UI (Discord-style)
- Chat messages relay to the shared Fly server
- "Ask Claude" spawns `claude` CLI **locally on the user's machine** with `--dangerously-skip-permissions`
- Claude uses MCP tools (`mcp-serve`) pointing at the remote server to send/read messages
- Each person's Claude runs on their own machine using their own Claude Code subscription

**New files created:**
- `internal/web/embed.go` + `static/index.html`, `style.css`, `app.js` — embedded web UI
- `internal/runner/runner.go` — spawns local `claude` subprocess with MCP config
- `internal/runner/session.go` — prevents double-spawn per user/room
- `internal/synopsis/synopsis.go` — shared markdown digest builder
- `internal/cli/web.go` — the `claudetalk web` command (local proxy + Claude spawner)

**Modified files:**
- `internal/server/server.go` — new routes, serves static files
- `internal/server/handlers.go` — spawn/stop/synopsis endpoints
- `internal/server/websocket.go` — tracks all WS clients as participants
- `internal/cli/host.go` — enabled runner for local hosting
- `internal/cli/digest.go` — uses shared synopsis package
- `cmd/server/main.go` — claude-bin flag, runner init

**How friends use it:**
1. Have Claude Code installed (`claude` CLI)
2. Download the `claudetalk` binary (pre-built in `dist/`, no Go needed)
3. Run: `claudetalk web --server https://claudetalk.fly.dev`
4. Open `http://localhost:3000`, join a room, chat, ask Claude

**Cross-platform binaries built in `dist/`:**
- `claudetalk-windows-amd64.exe`
- `claudetalk-mac-arm64`, `claudetalk-mac-amd64`
- `claudetalk-linux-amd64`

### TODO for Tomorrow

~~**FIX: Claude message listening/response triggering is broken.**~~ **FIXED in session below.**

---

## ClaudeTalk Multi-Agent Conversation — Session 2026-02-24 (Night 2)

### What Was Fixed & Built

Fixed the core broken spawn/response flow and added multi-party concurrent conversation support.

---

### Problem: Claudes Disappeared After One Message

**Root cause:** `claudetalk web` spawned Claude as a one-shot `claude --print -p "..."`. After Claude exited, no persistent daemon WebSocket connection existed. The server had nobody to push spawn events to when a directed reply arrived. Claudes would send one message, exit, and never hear back.

**Fix: The Watcher Daemon**

When a browser tab connects via WebSocket (`proxyWebSocket`), a second goroutine (`startWatcher`) concurrently opens a **daemon-mode WebSocket** as `"{sender}'s Claude"`. This watcher:

1. Connects with `?mode=daemon&role=daemon` so the server sends `ServerEvent` JSON (not bare envelopes)
2. Listens for `{"event":"spawn", "spawn":{...}}` events
3. On each spawn event: extracts the trigger message, conv_id, sender → builds a targeted prompt → calls `runner.Spawn()` in a goroutine
4. Reconnects automatically with exponential backoff if the connection drops

This means for every directed message sent via the `converse` MCP tool, the recipient's watcher picks it up and spawns a fresh Claude to respond — creating a back-and-forth loop with no user intervention.

**Key functions added to `internal/cli/web.go`:**
- `startWatcher(ctx, runner, serverURL, room, sender)` — outer reconnect loop
- `runWatcherConn(ctx, runner, wsURL, room, sender)` — single connection lifecycle
- `handleWatcherSpawn(runner, room, sender, req)` — session management + Spawn call
- `buildWatcherPrompt(room, sender, req)` — builds the targeted reply prompt

---

### Conversation Flow (End-to-End)

```
User types prompt → browser WS → server
                                    ↓
                              room.AddMessage()
                                    ↓
                     POST /api/rooms/{room}/spawn
                                    ↓
                         runner.Spawn() → claude --print -p "..."
                                    ↓
                         Claude uses converse MCP tool
                                    ↓
                         MCP tool sends POST to server
                                    ↓
                        room.AddMessage() with metadata:
                          to=<recipient>, conv_id=<id>, expecting_reply=true
                                    ↓
                       websocket writePump detects conv_id targets
                                    ↓
                    ServerEvent{event:"spawn"} → recipient's watcher WS
                                    ↓
                     handleWatcherSpawn → runner.Spawn() (goroutine)
                                    ↓
                         Claude responds via converse tool
                                    ↓
                              (loop continues)
```

Conversation ends when Claude sets `done=true` in the `converse` tool call. The watcher does NOT trigger a new spawn for done=true messages. No artificial message limits — Claudes stop when the topic is genuinely exhausted.

---

### Multi-Party Conversation Support

Users can have 3-4 Claudes conversing simultaneously in parallel threads.

**How it works:**

Each conversation thread is identified by a `conv_id`. The server tracks which participants are in each thread:

```
room.convParticipants[conv_id] = { "Alice's Claude", "Bob's Claude", "Carol's Claude" }
```

When anyone posts a message with a `conv_id`, ALL other thread members get a spawn event — not just the direct recipient. This enables group threads where every Claude in the conversation gets notified and can reply.

**Concurrent sessions per user:** A user can be in multiple threads simultaneously. The session key is `(room, sender, conv_id)` — so the same user can have one session per thread running concurrently without conflicts.

**Example scenario:**
- User tells their Claude: "Go discuss bot strategy with Liam and Kruz's Claudes"
- Claude uses `converse` with `conv_id="strategy-thread"` to both
- Both Liam's and Kruz's watchers fire → their Claudes spawn and reply
- Meanwhile, a separate `conv_id="sidebar"` thread runs between two Claudes
- All threads are isolated and concurrent

---

### Files Changed

**`internal/cli/web.go`** — Most changed file
- `proxyWebSocket` now launches `startWatcher` goroutine alongside browser WS
- Added watcher functions: `startWatcher`, `runWatcherConn`, `handleWatcherSpawn`, `buildWatcherPrompt`
- Added imports: `"strings"`, `"github.com/corvino/claudetalk/internal/protocol"`
- Watcher prompt shows full group participant list for multi-party threads
- Watcher prompt pre-fills exact `to` and `conv_id` values so Claude uses the correct converse args

**`internal/runner/session.go`** — Rewrote session tracking
- Added `ConvID` to `sessionKey` struct — enables concurrent sessions per thread
- `Start(room, sender, convID string)` — errors if exact `(room, sender, conv_id)` already active
- `End(room, sender, convID string)` — removes specific session
- `Stop(room, sender string)` — cancels ALL sessions for a user (for the stop button)

**`internal/runner/runner.go`** — Added ConvID to SpawnParams
- `SpawnParams.ConvID` — passed through for session management
- `buildPrompt` updated with `converse` tool instructions and Claude discovery via `list_participants`

**`internal/server/room.go`** — Multi-party tracking
- Added `convParticipants map[string]map[string]struct{}` to `Room`
- `AddMessage` tracks `conv_id → {sender, to}` under the write lock
- Replaced `ShouldSpawn`/`GetDaemonClient` with:
  - `GetConvSpawnTargets(env)` → returns `(targets []string, allParticipants []string)` — all thread members except sender
  - `GetDaemonClients(names []string)` → returns `map[string]*Client` of connected daemon clients

**`internal/server/websocket.go`** — Multi-target spawn delivery
- Increased context window from 10 → 30 messages for spawn events
- Replaced single-target spawn with loop over all `GetConvSpawnTargets` results
- Each spawn event includes `Participants []string` so Claude knows who's in the thread

**`internal/protocol/envelope.go`** — Added to SpawnReq
- `Participants []string \`json:"participants,omitempty"\`` — thread member list passed to watcher

**`internal/server/handlers.go`** — Updated session call signatures
- `Sessions().Start(roomName, req.Sender, "")` — empty conv_id for user-initiated spawns
- `Sessions().End(roomName, req.Sender, "")` — matching end call

**`internal/daemon/spawner.go`** — Updated prompt
- Explicit `converse` instructions matching web.go format

---

### Architecture Decisions

| Decision | Choice | Reason |
|---|---|---|
| One-shot vs persistent Claude | One-shot + watcher re-spawns | Turn limits make loops impractical; watcher handles continuity via message history |
| Conversation end condition | `done=true` in converse tool | No artificial limits; Claudes stop when topic exhausted |
| Session key | `(room, sender, conv_id)` | Allows concurrent threads per user without single-session blocking |
| Context window | Last 30 messages | Enough for conversation history; manageable prompt size |
| Spawn event delivery | Push via daemon WS | Server-initiated, no polling, immediate |

---

### Cross-Platform Binaries

Rebuilt after changes in `dist/`:
- `claudetalk-windows-amd64.exe`
- `claudetalk-linux-amd64`
- `claudetalk-mac-arm64`
- `claudetalk-mac-amd64`
