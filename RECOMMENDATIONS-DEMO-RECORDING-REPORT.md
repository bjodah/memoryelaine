# RECOMMENDATIONS: Demo Recording Feasibility Report

## TL;DR

| Demo | Feasibility | Recommended tool | Output |
|------|------------|-----------------|--------|
| 4 – CLI | ⭐⭐⭐ **Easiest** | VHS | `.gif` |
| 3 – TUI | ⭐⭐⭐ **Easiest** | VHS | `.gif` |
| 2 – Web UI | ⭐⭐ **Moderate** | Playwright → ffmpeg | `.mp4` / `.gif` |
| 1 – Emacs | ⭐ **Hardest** | VHS (`emacs -nw`) | `.gif` |

**Start with CLI and TUI** — they share a toolchain, share seed infrastructure, and each
takes under 30 minutes end-to-end. Tackle Web UI next. Leave Emacs for last.

---

## Common infrastructure (do once, reuse everywhere)

### 1. Build the binary

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .
```

### 2. Create `scripts/demo-seed-db.py`

A single Python script that populates `demos/demo.db` with realistic log rows.
Suggested rows (12 total):

| # | Model | User prompt | Status | Notes |
|---|-------|-------------|--------|-------|
| 1 | gpt-4o | "What is the capital of France?" | 200 | Short, fast |
| 2 | gpt-4o-mini | "Write a Python function to reverse a string" | 200 | Code response |
| 3 | gpt-4o | "Explain quantum entanglement in simple terms" | 200 | FTS demo keyword |
| 4 | gpt-4o | (bad API key) | 401 | Error demo |
| 5 | gpt-4o-mini | "Summarize the history of computing" | 200 | Longer response |
| 6 | claude-3-5-sonnet | "What is 2+2?" | 200 | Different model |
| 7 | gpt-4o | "Translate 'hello' to Spanish" | 200 | Short |
| 8 | gpt-4o | (upstream 500) | 500 | Error demo |
| 9 | gpt-o1 | "Solve this math problem step by step: …" | 200 | Has `<think>` / reasoning block |
| 10 | gpt-4o-mini | "List 5 sorting algorithms" | 200 | Longer |
| 11 | gpt-4o | "What time is it?" | 200 | Trivial |
| 12 | gpt-4o | "Generate a JSON schema for a user object" | 200 | JSON in response |

Row 9 must have a proper `<think>…</think>` block in the response body so the
stream-view `v` / `z` toggles in the TUI and Web UI are actually meaningful.

**Template for SSE response body (rows 1–3, 5–7, 10–12):**
```
data: {"id":"chatcmpl-001","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"delta":{"role":"assistant","content":"The capital of France is Paris."},"index":0}]}

data: [DONE]

```

**Template for row 9 (reasoning):**
```
data: {"id":"chatcmpl-009","object":"chat.completion.chunk","model":"o1","choices":[{"delta":{"reasoning_content":"Let me think about this…","content":""},"index":0}]}

data: {"id":"chatcmpl-009","object":"chat.completion.chunk","model":"o1","choices":[{"delta":{"reasoning_content":"","content":"The answer is 42."},"index":0}]}

data: [DONE]

```

### 3. Create `demos/demo-config.yaml`

```yaml
proxy:
  listen_addr: "127.0.0.1:18687"
  upstream_base_url: "https://api.openai.com"
  log_paths:
    - "/v1/chat/completions"
    - "/v1/completions"
management:
  listen_addr: "127.0.0.1:18677"
  auth:
    username: "admin"
    password: "changeme"
database:
  path: "./demos/demo.db"
logging:
  level: "warn"   # suppress noisy INFO lines during recording
```

### 4. Shared helper script `scripts/start-demo-server.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
./demos/memoryelaine serve --config demos/demo-config.yaml &
echo $! > /tmp/demo-server.pid
sleep 2
curl -sf http://127.0.0.1:18677/health > /dev/null && echo "Server ready"
```

### 5. Ensure VHS + ttyd are on PATH

```bash
export PATH="$HOME/go/bin:/usr/local/bin:$PATH"
# VHS_NO_SANDBOX=1 must be set when running as root
export VHS_NO_SANDBOX=1
```

---

## Per-demo assessment

### Demo 4 — CLI (`memoryelaine log`)

**Feasibility: ⭐⭐⭐ — Verified working end-to-end during exploration**

- `VHS_NO_SANDBOX=1 vhs demos/demo-cli.tape` produces a GIF in ~10 seconds
- No server required (the `log` command reads SQLite directly)
- Zero fragility: output is deterministic, no network calls, no timing sensitivity
- The demo showcases the query DSL, format options, and FTS — all compelling features

**Effort:** ~1 hour (write tape + seed script + minor polish)

---

### Demo 3 — TUI (`memoryelaine tui`)

**Feasibility: ⭐⭐⭐ — Verified infrastructure, one open question**

- Same VHS toolchain as CLI — trivial setup
- The TUI is a bubbletea fullscreen app; it renders correctly in VHS's ttyd terminal
- **Open question:** VHS injects keys via the terminal, not a PTY-level signal. Test that
  `j`/`k`/`Enter`/`v`/`f`/`q` are received correctly by bubbletea. If not, use `Down` /
  `Up` / `Return` VHS keywords which map to ANSI escape sequences.
- The `z` fold toggle and `x b`/`x c` export flow can be included as bonus scenes

**Effort:** ~1.5 hours (write tape, tune key timings, seed reasoning row)

---

### Demo 2 — Web UI

**Feasibility: ⭐⭐ — Verified working, some post-processing needed**

- Playwright with `--no-sandbox` successfully loaded the WebUI and recorded `.webm`
- Must use `127.0.0.1` (not `localhost`) — verified
- `ffmpeg` converts `.webm` → `.mp4` reliably
- GIF conversion is possible but large (use 960px width at 10fps for <8 MB)
- **Watch out for:** the JS app fetches `/api/logs` after page load; wait for `networkidle`
  before sending keyboard events. Tested and works.
- The recording script needs careful timing between scenes to avoid capturing
  half-rendered states

**Effort:** ~2–3 hours (write + tune Playwright script, post-process video)

---

### Demo 1 — Emacs client

**Feasibility: ⭐ — Hardest, but viable**

- Emacs 30.2.50 is available and the package loads cleanly
- **Terminal mode (`-nw`) via VHS** is the recommended path — avoids X11/xdotool fragility
- **Key challenge:** the `memoryelaine` buffer workflow requires `M-x memoryelaine` which
  in VHS is `Alt+x`. This may or may not be passed through correctly by ttyd/VHS. An
  alternative is to pre-call `(memoryelaine)` via `--eval` when launching Emacs, so the
  buffer opens immediately without an M-x step.
- **Second challenge:** the package makes async HTTP calls to the management server using
  `curl` subprocesses. These add ~50–200ms latency per API call. VHS `Sleep` durations
  need generous padding (3–5s) around buffer-opening steps.
- **Third challenge:** Emacs startup in a cold terminal takes ~2–4 seconds. The seed
  server must be running and responding before Emacs is launched.
- The GUI approach (Xvfb + ffmpeg + xdotool) was also verified but is harder to script
  reliably due to timing sensitivity.

**Effort:** ~4–6 hours (Emacs startup tuning, key injection testing, timing calibration)

---

## Recommended recording order

1. **CLI** — establish the seed script and config, validate end-to-end, get first GIF
2. **TUI** — reuse all infrastructure, add one VHS tape file
3. **Web UI** — start server, write Playwright script, tune scenes
4. **Emacs** — tackle last, most iteration needed

---

## Helper scripts to create

| Script | Purpose |
|--------|---------|
| `scripts/demo-seed-db.py` | Creates `demos/demo.db` with 12 realistic rows |
| `scripts/start-demo-server.sh` | Launches server with demo config, writes PID file |
| `scripts/stop-demo-server.sh` | Kills server by PID |
| `scripts/record-webui.py` | Playwright recording script for Web UI demo |
| `demos/demo-config.yaml` | Config pointing to `demos/demo.db`, port 18677/18687 |
| `demos/demo-cli.tape` | VHS tape for CLI demo |
| `demos/demo-tui.tape` | VHS tape for TUI demo |
| `demos/demo-emacs.tape` | VHS tape for Emacs demo |

---

## Tool versions confirmed present

| Tool | Version | Notes |
|------|---------|-------|
| VHS | 0.11.0 | `~/go/bin/vhs`; needs `VHS_NO_SANDBOX=1` as root |
| ttyd | 1.7.4 | `/usr/local/bin/ttyd`; VHS dependency |
| asciinema | 3.2.0 | Alternative to VHS; config fix needed (see below) |
| Playwright (Python) | 1.58.0 | Chromium browser present |
| ffmpeg | 7.1.3 | With libx264, GIF, x11grab support |
| Emacs | 30.2.50 | `/opt-3/emacs-30-lucid/bin/emacs` |
| Xvfb | 21.1.16 | For Emacs GUI approach |
| xdotool | 3.20160805 | For Emacs GUI approach |
| imagemagick | 7.1.1.43 | `identify`, `convert` for GIF inspection |
| agg | 0.3.1 | Python; converts asciinema `.cast` → `.gif` (alternative pipeline) |

### asciinema config fix required

The installed asciinema config at `~/.config/asciinema/config.toml` uses invalid key
names (`C-<f12>`) that cause a startup error. Fix:

```toml
# ~/.config/asciinema/config.toml
[session]
prefix_key = "C-\\"
pause_key = "C-p"
```

---

## Format recommendations for the README

- **CLI demo** → `.gif` (small, 80–150 KB, embeds inline in GitHub Markdown)
- **TUI demo** → `.gif` (slightly larger, ~200–400 KB)
- **Web UI demo** → `.mp4` referenced via `<video>` tag, or hosted on a CDN; GIF at
  reduced resolution (960px) if inline embedding is required
- **Emacs demo** → `.gif` via VHS terminal approach

GitHub renders `<video>` tags in `README.md` starting with GitHub Flavored Markdown
support for video. For maximum compatibility, provide both `.mp4` and a
thumbnail/poster image, and link to the `.mp4` from the README.
