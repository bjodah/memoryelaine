# RECOMMENDATIONS: Demo Recording Feasibility Report

## TL;DR

| Demo | Feasibility | Recommended tool | Output | Status |
|------|------------|-----------------|--------|--------|
| 4 – CLI | ⭐⭐⭐ **Easiest** | VHS | `.gif` + `.mp4` | ✅ **Done** |
| 3 – TUI | ⭐⭐⭐ **Easiest** | VHS | `.gif` + `.mp4` | ✅ **Done** |
| 2 – Web UI | ⭐⭐ **Moderate** | Playwright → ffmpeg | `.mp4` / `.gif` | ✅ **Done** |
| 1 – Emacs | ⭐⭐ **Moderate** | VHS (`emacs -nw`) or Xvfb+ffmpeg (GUI) | `.gif` / `.mp4` | ✅ **Done** |

**All recordings are complete.** Output files in `demos/`:
- `demos/demo-cli.gif` (903 KB, 1200×600) + `demos/demo-cli.mp4` (553 KB)
- `demos/demo-tui.gif` (469 KB, 1600×700) + `demos/demo-tui.mp4` (251 KB)
- `demos/demo-emacs-tui.gif` (324 KB, 1200×700) + `demos/demo-emacs-tui.mp4` (277 KB)
- `demos/demo-emacs-gui.gif` (725 KB, 960×600) + `demos/demo-emacs-gui.mp4` (788 KB, 1280×800)
- `demos/demo-webui.gif` (1.7 MB, 960×600) + `demos/demo-webui.mp4` (603 KB, 1280×800)

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

**Feasibility: ⭐⭐ — ✅ Recording complete**

- Playwright with `--no-sandbox` successfully recorded headless Chromium → `.webm` → `.mp4` + `.gif`
- **Output:** `demos/demo-webui.mp4` (603 KB, 1280×800, 24.96s) + `demos/demo-webui.gif` (1.7 MB, 960×600)

**Demo covers:** table with 12 colored entries → `?` help overlay → detail panel → conversation view →
`quantum` FTS query → `is:error` query filter → `R` recording toggle.

**Pitfalls encountered:**
- Server and Playwright script **must run in the same shell session** — the server is a
  background job that dies when its shell session ends; separate bash tool calls get a new
  session, so the server is already dead when Python runs. Solution: `server & PY_SCRIPT; kill $!`
- **Focus trap after query `Enter`:** focus stays on the query input; `j` key types into the
  filter instead of navigating rows. Fix: press `Escape` after `Enter` to blur before navigating.
- The `v` toggle (raw/assembled) only works for SSE-streaming entries (`assembled_available=true`);
  non-streaming entries show a toast. Replaced with `c` (conversation view) in the demo.
- Use credentials matching `demos/demo-config.yaml` (`demo`/`demo1234`), not the plan template
  values (`admin`/`changeme`).

**Effort:** ~1 hour (including debugging focus trap and session lifetime issues)

---

### Demo 1 — Emacs client

**Feasibility: ⭐⭐ — ✅ Recording complete (both approaches)**

Both terminal (`-nw`) and GUI approaches were executed end-to-end:

**Approach A: Terminal mode (`-nw`) via VHS — `demos/demo-emacs-tui.gif` + `.mp4`**
- Emacs 30.2.50 loads the `memoryelaine` package cleanly in `-nw` mode
- VHS tape automates key injection via ttyd; using `-l ./demos/demo-emacs-init.el` avoids
  all quoting issues with `--eval` and inner double-quote escaping
- Same toolchain as CLI/TUI — minimal additional setup
- **Output:** `demos/demo-emacs-tui.gif` (324 KB, 1200×700) + `demos/demo-emacs-tui.mp4` (277 KB)

**Approach B: GUI via Xvfb + openbox + ffmpeg + xdotool — `demos/demo-emacs-gui.gif` + `.mp4`**
- **Verified working** — `*memoryelaine*` buffer populated with all 12 log entries in recording
- The automation showed M-x workflow, navigation, detail view, and "quantum" search filter
- **Output:** `demos/demo-emacs-gui.mp4` (788 KB, 1280×800) + `demos/demo-emacs-gui.gif` (725 KB, 960px)

**Bug found and fixed during recording:** The Emacs package's `memoryelaine-show--insert-headers`
function did not handle JSON `null` headers (which `json-parse-string` represents as `:null`).
The `dolist` call failed with "Wrong type argument: listp, :null" when `req_headers` or
`resp_headers` were null in the database (as in seeded demo data). Fixed by adding
`(eq headers :null)` to the guard condition, and the "Error: :null" display issue was fixed
similarly. All 55 Emacs unit tests still pass.

**Pitfalls encountered:**
- Same pixel dimension / theme / PATH issues as CLI
- Use `-l ./demos/demo-emacs-init.el` instead of `--eval` to avoid quoting complexity
  when inner double-quotes need to appear in the eval string
- The database cursor starts at the first entry (row 12, most recent); 2 Down presses
  navigate to row 10 (not row 3); the "quantum" search filter still demonstrates FTS
- `EMACS_SOCKET` env var must be unset for GUI approach — use `env -u EMACS_SOCKET`
- `xdotool windowfocus --sync` prints a `BadMatch` X error that is non-fatal
- The `kill -INT` sent to ffmpeg causes exit code 1 from the script but the mp4 is intact;
  GIF conversion must be run separately if the script exits early
- The GIF conversion is done via two-pass palette approach:
  `fps=10,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=128[p];[s1][p]paletteuse`

**Effort:** ~2 hours (both approaches, including bug fix)

---

## Recommended recording order

1. **CLI** — establish the seed script and config, validate end-to-end, get first GIF
2. **TUI** — reuse all infrastructure, add one VHS tape file
3. **Web UI** — start server, write Playwright script, tune scenes
4. **Emacs** — tackle last, most iteration needed

---

## Helper scripts created

| Script | Purpose | Status |
|--------|---------|--------|
| `scripts/demo-seed-db.py` | Creates `demos/demo.db` with 12 realistic rows | ✅ Created |
| `scripts/start-demo-server.sh` | Launches server with demo config, writes PID file | ✅ Created |
| `scripts/stop-demo-server.sh` | Kills server by PID | ✅ Created |
| `scripts/record-webui.py` | Playwright recording script for Web UI demo | ✅ Created |
| `scripts/record-emacs-gui.sh` | Shell + xdotool script for GUI Emacs demo | ✅ Created |
| `demos/demo-config.yaml` | Config pointing to `demos/demo.db`, port 18677/18687 | ✅ Created |
| `demos/demo-cli.tape` | VHS tape for CLI demo | ✅ Created |
| `demos/demo-tui.tape` | VHS tape for TUI demo | ✅ Created |
| `demos/demo-emacs-tui.tape` | VHS tape for Emacs terminal (`-nw`) demo | ✅ Created |
| `demos/demo-emacs-init.el` | Emacs Lisp init loaded by TUI tape (avoids quoting) | ✅ Created |

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
