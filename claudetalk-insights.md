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
