# PLAN-01-DEMO-EMACS: Recording an Emacs Client Demo

## Goal

Produce an animated demo (`.gif` or `.mp4`) of the `memoryelaine` Emacs client browsing
captured OpenAI proxy logs — showing the search buffer, navigating entries, and viewing
request/response detail.

Two distinct approaches are available — choose either or produce both:

| | **Approach A: Terminal (`-nw`) via VHS** | **Approach B: GUI via Xvfb + ffmpeg** |
|---|---|---|
| Output format | `.gif` (or `.mp4` via ffmpeg) | `.mp4` |
| Automation | VHS tape DSL | Shell script + `xdotool` |
| Setup complexity | Low | Medium |
| Visual style | Styled terminal (Monokai, etc.) | Real Emacs GUI (toolbar, menu bar) |
| Reproducibility | High (deterministic tape) | Medium (timing-sensitive) |
| Verified working | ✅ | ✅ |

---

## Verified environment facts

| Item | Status |
|------|--------|
| Emacs 30.2.50 (Lucid build) | ✅ `/opt-3/emacs-30-lucid/bin/emacs` |
| `memoryelaine` package loads | ✅ `(require 'memoryelaine)` works in batch mode |
| Xvfb virtual display | ✅ `Xvfb :99 -screen 0 1280x800x24` |
| openbox window manager | ✅ installed via `apt-get install openbox` — required for xdotool EWMH |
| xdotool (GUI key injection) | ✅ installed via `apt-get install xdotool` |
| ffmpeg x11grab screen capture | ✅ tested end-to-end, produces `.mp4` |
| VHS terminal recorder | ✅ `~/go/bin/vhs` — works with `VHS_NO_SANDBOX=1` |
| ttyd | ✅ installed at `/usr/local/bin/ttyd` (required by VHS) |
| `EMACS_SOCKET` env var pitfall | ⚠️ must `unset EMACS_SOCKET` before launching GUI Emacs |

> **Critical note on `EMACS_SOCKET`:** the sandbox environment sets `EMACS_SOCKET` pointing
> to a running daemon. Without clearing it, the Lucid binary connects to that daemon instead
> of opening a new GUI frame, then exits silently. Always launch demo Emacs with
> `env -u EMACS_SOCKET`.

---

## Common prerequisites (both approaches)

### Build the binary

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .
```

### Create the demo config (`demos/demo-config.yaml`)

```yaml
proxy:
  listen_addr: "127.0.0.1:18687"
  upstream_base_url: "https://api.openai.com"
  timeout_minutes: 5
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
  level: "warn"
```

### Seed the database (`scripts/demo-seed-db.py`)

```python
#!/usr/bin/env python3
"""Insert realistic demo log rows directly into the memoryelaine SQLite database."""
import sqlite3, json, time, sys

DB_PATH = sys.argv[1] if len(sys.argv) > 1 else "./demos/demo.db"
db = sqlite3.connect(DB_PATH)

# Let the server run once first to create the schema, then stop it and seed.
# Alternatively, start the server, seed the DB, restart — the schema must already exist.

convs = [
    ("gpt-4o",      "Explain quantum entanglement in simple terms.",
                    "Quantum entanglement is a phenomenon where two particles become linked — "
                    "measuring one instantly reveals the state of the other, no matter the distance."),
    ("gpt-4o-mini", "Write a Python function to reverse a string.",
                    "```python\ndef reverse(s: str) -> str:\n    return s[::-1]\n```"),
    ("gpt-3.5-turbo","What is the capital of France?",
                    "The capital of France is Paris."),
    ("gpt-4o",      "Summarize the French Revolution briefly.",
                    "The French Revolution (1789–1799) abolished the absolute monarchy, "
                    "proclaimed the Declaration of the Rights of Man, and set the stage for Napoleon."),
    ("gpt-4o-mini", "How does HTTP/2 differ from HTTP/1.1?",
                    "HTTP/2 adds: multiplexing, header compression (HPACK), server push, "
                    "and binary framing — reducing latency significantly."),
    ("gpt-4o",      "Translate 'hello world' into French.",  "Bonjour le monde."),
    ("gpt-3.5-turbo","Give me a haiku about software bugs.",
                    "Late-night deploy fails /\nNull pointer in the stack trace /\nCoffee cup runs dry"),
]
ts = int(time.time() * 1000) - len(convs) * 20000
for i, (model, prompt, reply) in enumerate(convs):
    req  = json.dumps({"model": model, "messages": [{"role": "user", "content": prompt}]})
    resp = json.dumps({"id": f"chatcmpl-demo{i:04d}", "object": "chat.completion",
                       "model": model,
                       "choices": [{"index": 0,
                                    "message": {"role": "assistant", "content": reply},
                                    "finish_reason": "stop"}],
                       "usage": {"prompt_tokens": 20, "completion_tokens": 40, "total_tokens": 60}})
    db.execute("""
        INSERT INTO openai_logs
          (ts_start, ts_end, duration_ms, client_ip, request_method, request_path,
           upstream_url, status_code, req_body, resp_body, req_bytes, resp_bytes,
           req_text, resp_text)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
    """, (ts + i*20000, ts + i*20000 + 1800, 1800, "127.0.0.1", "POST",
          "/v1/chat/completions", "https://api.openai.com/v1/chat/completions",
          200, req, resp, len(req), len(resp), prompt, reply))
db.commit()
db.close()
print(f"Seeded {len(convs)} rows into {DB_PATH}")
```

> **Important:** start the server at least once before seeding so its migration creates the
> schema, then stop it, run the seed script, and restart.

### Start the management server

```bash
./demos/memoryelaine serve --config demos/demo-config.yaml &
SERVER_PID=$!
sleep 2
curl -s http://127.0.0.1:18677/health   # verify {"status":"ok",...}
```

---

## Approach A: Terminal mode (`-nw`) via VHS

Running Emacs with `-nw` inside a VHS-managed `ttyd` terminal produces a clean, styled
GIF that looks like a normal terminal recording. No X server or window manager is needed.

### VHS tape (`demos/demo-emacs-tui.tape`)

```tape
Output demos/demo-emacs-tui.gif
Set Shell "bash"
Set FontSize 14
Set Width 220
Set Height 52
Set Theme "Monokai"
Set TypingSpeed 60ms

# Launch Emacs in terminal mode with the memoryelaine package
Type "EMACS=/opt-3/emacs-30-lucid/bin/emacs"
Enter
Type "$EMACS -nw -Q -L ./emacs-memoryelaine \\"
Enter
Type "  --eval '(progn (require (quote memoryelaine)) (setq memoryelaine-base-url \"http://127.0.0.1:18677\" memoryelaine-username \"admin\" memoryelaine-password \"changeme\") (memoryelaine))'"
Enter
Sleep 4s

# Now inside Emacs — the *memoryelaine* search buffer should be open
# Navigate down to a row
Down
Down
Sleep 500ms

# Open detail view (RET)
Enter
Sleep 3s

# Go back to list (q or Escape)
Ctrl+x
Ctrl+k

# Type a search query (s key)
Type "s"
Type "quantum"
Enter
Sleep 3s

# Quit Emacs
Ctrl+x
Ctrl+c
```

> **Tips:**
> - Use `--eval '...(memoryelaine)...'` to call `M-x memoryelaine` automatically on startup —
>   avoids the VHS `Alt+x` minibuffer timing uncertainty.
> - Add `Sleep 3s` after any step that triggers an async HTTP curl call.
> - Increase `Set Height` if the tabulated list truncates columns.

### Run the tape

```bash
cd /work
export PATH="$HOME/go/bin:$PATH"
VHS_NO_SANDBOX=1 vhs demos/demo-emacs-tui.tape
# output: demos/demo-emacs-tui.gif
```

### Optional: convert to MP4

```bash
ffmpeg -i demos/demo-emacs-tui.gif \
       -movflags faststart -pix_fmt yuv420p \
       -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" \
       demos/demo-emacs-tui.mp4
```

---

## Approach B: GUI via Xvfb + openbox + ffmpeg + xdotool

This produces a recording of the real Emacs GUI window — toolbar, menu bar, proportional
fonts — running inside a virtual framebuffer. The automation script below was verified
end-to-end: xdotool successfully opened the `*memoryelaine*` buffer and data loaded.

### Setup steps

```bash
# Ensure tools are installed (one-time)
apt-get install -y xdotool openbox
# openbox is required — Lucid Emacs's windows are only EWMH-aware when a
# conforming WM is running, which xdotool needs for windowfocus.
```

### Full automation script (`scripts/record-emacs-gui.sh`)

```bash
#!/usr/bin/env bash
set -euo pipefail

DISPLAY_NUM=99
DISPLAY=:${DISPLAY_NUM}
EMACS=/opt-3/emacs-30-lucid/bin/emacs
EMACS_LOAD_PATH=./emacs-memoryelaine
OUT=demos/demo-emacs-gui.mp4

# ── 1. Virtual display ────────────────────────────────────────────────────────
Xvfb :${DISPLAY_NUM} -screen 0 1280x800x24 &
XVFB_PID=$!
sleep 1

# ── 2. Window manager (EWMH required for xdotool windowfocus) ─────────────────
DISPLAY=${DISPLAY} openbox &
OPENBOX_PID=$!
sleep 2

# ── 3. Start ffmpeg capture ───────────────────────────────────────────────────
DISPLAY=${DISPLAY} ffmpeg -y -f x11grab -r 25 -s 1280x800 -i :${DISPLAY_NUM} \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
  "${OUT}" &
FFMPEG_PID=$!
sleep 1

# ── 4. Launch Emacs GUI ───────────────────────────────────────────────────────
# CRITICAL: unset EMACS_SOCKET — the sandbox has it pointing to an existing
# daemon; without this, the Lucid binary connects to that daemon and exits.
setsid env -u EMACS_SOCKET DISPLAY=${DISPLAY} \
  ${EMACS} -Q --no-desktop -geometry 140x42+0+0 \
  -L ${EMACS_LOAD_PATH} \
  --eval '(progn
    (require (quote memoryelaine))
    (setq memoryelaine-base-url "http://127.0.0.1:18677"
          memoryelaine-username "admin"
          memoryelaine-password "changeme"))' \
  > /tmp/emacs-gui-demo.log 2>&1 &
sleep 8   # wait for Emacs frame and initial HTTP request to settle

# ── 5. Find the main Emacs frame via xdotool ─────────────────────────────────
# xdotool search --class "Emacs" returns 3 window IDs for a Lucid frame;
# the one whose name contains "scratch" or "memoryelaine" is the main frame.
WIN=""
for wid in $(DISPLAY=${DISPLAY} xdotool search --class "Emacs" 2>/dev/null); do
  name=$(DISPLAY=${DISPLAY} xdotool getwindowname "$wid" 2>/dev/null || true)
  if [[ "$name" == *"scratch"* || "$name" == *"memoryelaine"* || "$name" == *"GNU Emacs"* ]]; then
    WIN="$wid"
    break
  fi
done
echo "Emacs window ID: ${WIN}"

# ── Helper: send a key to the Emacs window ───────────────────────────────────
send_key() {
  DISPLAY=${DISPLAY} xdotool windowfocus --sync "${WIN}" 2>/dev/null || true
  sleep 0.2
  DISPLAY=${DISPLAY} xdotool key --window "${WIN}" "$@"
}
send_type() {
  DISPLAY=${DISPLAY} xdotool windowfocus --sync "${WIN}" 2>/dev/null || true
  sleep 0.2
  DISPLAY=${DISPLAY} xdotool type --window "${WIN}" --delay 60 "$@"
}

# ── 6. Open the memoryelaine search buffer (M-x memoryelaine) ─────────────────
# Note: Emacs was started with --eval that loads the package but does NOT call
# (memoryelaine) — we call it via M-x so the demo shows that UX.
send_key alt+x
sleep 0.5
send_type "memoryelaine"
sleep 0.3
send_key Return
sleep 5   # wait for async curl call to complete and buffer to populate

# ── 7. Navigate and open detail view ─────────────────────────────────────────
send_key Down
sleep 0.3
send_key Down
sleep 0.3
send_key Return          # open detail view
sleep 4                   # wait for detail HTTP request

# ── 8. Navigate back to list ──────────────────────────────────────────────────
send_key q
sleep 1

# ── 9. Filter by query (s key) ────────────────────────────────────────────────
send_key s               # edit query
sleep 0.3
send_type "quantum"
send_key Return
sleep 4

# ── 10. Quit Emacs ────────────────────────────────────────────────────────────
send_key ctrl+x
sleep 0.2
send_key ctrl+c
sleep 2

# ── 11. Stop recording ────────────────────────────────────────────────────────
kill "${FFMPEG_PID}" 2>/dev/null || true
wait "${FFMPEG_PID}" 2>/dev/null || true
kill "${OPENBOX_PID}" 2>/dev/null || true
kill "${XVFB_PID}" 2>/dev/null || true

echo "Done: ${OUT}"
```

### Run it

```bash
bash scripts/record-emacs-gui.sh
# output: demos/demo-emacs-gui.mp4
```

### Key findings from verification

| Finding | Detail |
|---------|--------|
| `EMACS_SOCKET` must be unset | The sandbox sets this; Lucid connects to daemon and exits silently |
| `setsid` instead of `nohup` | `nohup &` in a bash subshell doesn't survive the subshell exit; `setsid` detaches correctly |
| `openbox` required | Without a EWMH WM, `xdotool windowfocus` fails with `_NET_ACTIVE_WINDOW not supported` |
| Window lookup | `xdotool search --class "Emacs"` returns 3 IDs; filter by window name |
| `windowfocus` prints `BadMatch` | This X error is non-fatal — key events still arrive |
| Refresh key is `g` | Not `r` or `R`; `R` toggles recording mode |
| Allow ≥5s after `memoryelaine` M-x | Package uses async curl subprocesses; buffer takes time to populate |
| `-Q` flag | Required to skip the sandbox's heavy user config (dark theme, use-package noise) |

---

## Expected output

A ~20–30 second GIF or MP4 showing:
1. Emacs opens (scratch buffer for `-nw`, or GUI frame for Approach B)
2. `M-x memoryelaine` opens the `*memoryelaine*` search buffer listing log entries
3. User navigates down and opens a detail view
4. User types a filter query and the list refreshes
5. Emacs exits

Both approaches produce a compelling, authentic demo of the Emacs client.
