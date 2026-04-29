# RECOMMENDATIONS: Demo Recording Feasibility Report

## TL;DR

| Demo | Feasibility | Recommended tool | Output | Status |
|------|------------|-----------------|--------|--------|
| 4 – CLI | ⭐⭐⭐ **Easiest** | VHS | `.gif` + `.mp4` | ✅ **Done** |
| 3 – TUI | ⭐⭐⭐ **Easiest** | VHS | `.gif` + `.mp4` | ✅ **Done** |
| 2 – Web UI | ⭐⭐ **Moderate** | Playwright → ffmpeg | `.mp4` / `.gif` | ⏳ Pending |
| 1 – Emacs | ⭐⭐ **Moderate** | VHS (`emacs -nw`) or Xvfb+ffmpeg (GUI) | `.gif` / `.mp4` | ⏳ Pending |

**CLI and TUI recordings are complete.** Output files in `demos/`:
- `demos/demo-cli.gif` (903 KB, 1200×600) + `demos/demo-cli.mp4` (553 KB)
- `demos/demo-tui.gif` (469 KB, 1600×700) + `demos/demo-tui.mp4` (251 KB)

---

## Common infrastructure (do once, reuse everywhere)

### 1. Build the binary

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .
```

### 2. `scripts/demo-seed-db.py` ✅ created

A Python script that populates `demos/demo.db` with 12 realistic log rows.
Run: `python3 scripts/demo-seed-db.py --out demos/demo.db`

> **Note:** Row 4 uses `status_code=400` (not 401) because the TUI status filter
> uses **exact** match (`status_code = 400`). A 401 row would not appear when the
> filter is set to 400. Similarly row 8 uses `status_code=500` exactly.
> The original plan listed a 401 row — this was corrected in the seed script.

| # | Model | User prompt | Status | Notes |
|---|-------|-------------|--------|-------|
| 1 | gpt-4o | "What is the capital of France?" | 200 | Short, fast |
| 2 | gpt-4o-mini | "Write a Python function to reverse a string" | 200 | Code response |
| 3 | gpt-4o | "Explain quantum entanglement in simple terms" | 200 | FTS demo keyword |
| 4 | gpt-4o | (bad request) | **400** | Error demo — exact match for TUI filter |
| 5 | gpt-4o-mini | "Summarize the history of computing" | 200 | Longer response |
| 6 | claude-3-5-sonnet | "What is 2+2?" | 200 | Different model |
| 7 | gpt-4o | "Translate 'hello' to Spanish" | 200 | Short |
| 8 | gpt-4o | (upstream error) | **500** | Error demo — exact match for TUI filter |
| 9 | o1 | "Solve: 10th Fibonacci number" | 200 | SSE reasoning_content chunks |
| 10 | gpt-4o-mini | "List 5 sorting algorithms" | 200 | Longer |
| 11 | gpt-4o | "What time is it?" | 200 | Trivial |
| 12 | gpt-4o | "Generate a JSON schema for a user object" | 200 | JSON in response |

Row 9 uses `reasoning_content` SSE chunks (not `<think>` tags) to trigger the
`v` stream-view toggle and `z` fold toggle in the TUI.

### 3. `demos/demo-config.yaml` ✅ created

> **Critical:** Do NOT use `username: admin / password: changeme` — the config
> validator emits `slog.Warn` unconditionally for default credentials, which appears
> as a noisy line in every CLI command output during recording.

```yaml
management:
  auth:
    username: "demo"
    password: "demo1234"
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

> **Critical:** ttyd is at `/opt-3/ttyd/bin/ttyd`, **not** `/usr/local/bin/ttyd`.
> VHS will silently fail or error if ttyd is not on PATH.

```bash
export PATH="$HOME/go/bin:/opt-3/ttyd/bin:$PATH"
export VHS_NO_SANDBOX=1   # required when running as root
```

### 6. VHS dimension gotcha

`Set Width` and `Set Height` in VHS tape files are **pixel** dimensions, not
terminal character columns/rows. The minimum is 120×120 pixels. Typical values:
- CLI: `Set Width 1200` `Set Height 600`
- TUI: `Set Width 1600` `Set Height 700`

At FontSize=14, Width=1200 ≈ 140 terminal columns; Height=600 ≈ 35 rows.

### 7. Theme name gotcha

`Set Theme "Monokai"` is **not valid** — VHS will error. Use:
- `"Monokai Remastered"` (for CLI / warmer look)
- `"Dracula"` (for TUI / cooler dark look)

---

## Per-demo assessment

### Demo 4 — CLI (`memoryelaine log`)

**Feasibility: ⭐⭐⭐ — ✅ Recording complete**

- `VHS_NO_SANDBOX=1 vhs demos/demo-cli.tape` produces a GIF in ~60s (first run downloads Chromium)
- No server required (the `log` command reads SQLite directly)
- Zero fragility: output is deterministic, no network calls, no timing sensitivity
- The demo showcases FTS, status filter, JSON/JSONL format options — compelling features
- **Output:** `demos/demo-cli.gif` (903 KB, 1200×600) + `demos/demo-cli.mp4` (553 KB)

**Pitfalls encountered:**
- `Set Width`/`Set Height` are pixels not characters; must be ≥120×120
- `Set Theme "Monokai"` is invalid — use `"Monokai Remastered"`
- Default credentials in config emit a `slog.Warn` line before every command
- ttyd is at `/opt-3/ttyd/bin/ttyd`, must add to PATH

---

### Demo 3 — TUI (`memoryelaine tui`)

**Feasibility: ⭐⭐⭐ — ✅ Recording complete (open question resolved)**

- Same VHS toolchain as CLI — same pitfalls apply (pixel dims, theme name, PATH)
- The TUI is a bubbletea fullscreen app; renders correctly in VHS's ttyd terminal
- **Open question resolved:** `j`/`k`/`Enter`/`v`/`f`/`q`/`Escape` all work
  — bubbletea receives them correctly via VHS key injection
- The `z` fold toggle and `x b`/`x c` export flow can be included as bonus scenes
- **Output:** `demos/demo-tui.gif` (469 KB, 1600×700) + `demos/demo-tui.mp4` (251 KB)

**Pitfalls encountered:**
- Same pixel dimension / theme / PATH issues as CLI
- Pagination (`n`/`p`) is a no-op with 12 rows (limit=50); removed from tape
- Status filter uses exact match: seed data must use `status_code=400` and `status_code=500`
  (not 401 as originally planned)

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

**Feasibility: ⭐⭐ — Two verified approaches; terminal path is simpler**

Both the terminal (`-nw`) and GUI approaches were verified end-to-end:

**Approach A: Terminal mode (`-nw`) via VHS**
- Emacs 30.2.50 loads the `memoryelaine` package cleanly in `-nw` mode
- VHS tape automates key injection via ttyd; use `--eval '(memoryelaine)'` on startup to
  skip the M-x minibuffer step and avoid timing issues
- Same toolchain as CLI/TUI demos — minimal additional setup
- Add generous `Sleep 4s–5s` around steps that trigger async HTTP curl calls

**Approach B: GUI via Xvfb + openbox + ffmpeg + xdotool**
- **Verified working** — `*memoryelaine*` buffer populated with 5 log entries in screenshot
- Requires: `openbox` (EWMH WM for xdotool), `env -u EMACS_SOCKET` (sandbox sets this var
  to an existing daemon socket — without clearing it, Lucid exits silently), `setsid` (not
  `nohup`) to detach properly
- Window lookup: `xdotool search --class "Emacs"` returns 3 IDs; filter by window name
- `xdotool windowfocus --sync $WIN` prints a `BadMatch` X error that is non-fatal
- More timing-sensitive than Approach A but produces a more visually impressive recording

**Common pitfalls:**
- `EMACS_SOCKET` env var must be unset — this is the #1 silent failure cause
- Refresh key is `g` (not `r`); `R` toggles recording mode; `s` edits the query
- Allow ≥5 seconds after opening the buffer for async HTTP to complete

**Effort:** ~3–4 hours (Approach A); ~5–6 hours (Approach B or both)

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
| `scripts/record-emacs-gui.sh` | Shell + xdotool script for GUI Emacs demo |
| `demos/demo-config.yaml` | Config pointing to `demos/demo.db`, port 18677/18687 |
| `demos/demo-cli.tape` | VHS tape for CLI demo |
| `demos/demo-tui.tape` | VHS tape for TUI demo |
| `demos/demo-emacs-tui.tape` | VHS tape for Emacs terminal (`-nw`) demo |

---

## Tool versions confirmed present

| Tool | Version | Location | Notes |
|------|---------|----------|-------|
| VHS | 0.11.0 | `~/go/bin/vhs` | Install: `go install github.com/charmbracelet/vhs@latest`; needs `VHS_NO_SANDBOX=1` as root |
| ttyd | 1.7.7 | `/opt-3/ttyd/bin/ttyd` | **Not** at `/usr/local/bin` — must add `/opt-3/ttyd/bin` to PATH |
| asciinema | 3.2.0 | `$PATH` | Alternative to VHS; config fix needed (see below) |
| Playwright (Python) | 1.58.0 | pip | Chromium browser present |
| ffmpeg | 7.1.3 | `/usr/bin/ffmpeg` | With libx264, GIF, x11grab support |
| Emacs | 30.2.50 | `/opt-3/emacs-30-lucid/bin/emacs` | For Emacs GUI approach |
| Xvfb | 21.1.16 | `/usr/bin/Xvfb` | For Emacs GUI approach |
| xdotool | 3.20160805 | `/usr/bin/xdotool` | For Emacs GUI approach |
| openbox | (installed) | `/usr/bin/openbox` | EWMH WM — required for xdotool in Xvfb |
| imagemagick | 7.1.1.43 | `/usr/bin/identify` | `identify`, `convert` for GIF inspection |
| agg | 0.3.1 | pip | Converts asciinema `.cast` → `.gif` (alternative pipeline) |

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

- **CLI demo** → `.gif` embeds inline in GitHub Markdown (actual: 903 KB at 1200×600); or
  `.mp4` via `<video>` tag if file size matters (actual: 553 KB)
- **TUI demo** → `.gif` (actual: 469 KB at 1600×700) or `.mp4` (actual: 251 KB)
- **Web UI demo** → `.mp4` referenced via `<video>` tag, or hosted on a CDN; GIF at
  reduced resolution (960px) if inline embedding is required
- **Emacs demo** → `.gif` via VHS terminal approach

GitHub renders `<video>` tags in `README.md` starting with GitHub Flavored Markdown
support for video. For maximum compatibility, provide both `.mp4` and a
thumbnail/poster image, and link to the `.mp4` from the README.
