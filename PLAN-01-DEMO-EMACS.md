# PLAN-01-DEMO-EMACS: Recording an Emacs Client Demo

## Goal

Produce an animated demo (`.gif` or `.mp4`) of the `memoryelaine` Emacs client browsing
captured OpenAI proxy logs — showing the search buffer, navigating entries, and viewing
request/response detail.

---

## Verified environment facts

| Item | Status |
|------|--------|
| Emacs 30.2.50 (Lucid build) | ✅ `/opt-3/emacs-30-lucid/bin/emacs` |
| `memoryelaine` package loads | ✅ `(require 'memoryelaine)` works in batch mode |
| Xvfb virtual display | ✅ `xvfb-run` / manual `Xvfb :99` |
| xdotool (GUI key injection) | ✅ installed via `apt-get install xdotool` |
| ffmpeg x11grab screen capture | ✅ tested, produces `.mp4` |
| VHS terminal recorder | ✅ `~/go/bin/vhs` — works with `VHS_NO_SANDBOX=1` |
| ttyd | ✅ installed at `/usr/local/bin/ttyd` |

---

## Recommended approach: VHS with Emacs `-nw` (no-window / terminal mode)

Running Emacs in **terminal mode** (`-nw`) inside a VHS tape gives a clean, styled terminal
GIF without needing X11/xdotool automation. VHS controls key input via its tape DSL, so the
demo is fully reproducible.

### Why prefer this over the GUI approach

- No X11 flicker or window-manager chrome
- VHS output looks polished (configurable font, color theme, dimensions)
- Fully scriptable — no timing fragility with xdotool
- Consistent with the TUI demo's approach

### Alternative: GUI + Xvfb + ffmpeg

If a graphical Emacs window (proportional fonts, fringe decorations) is desired, use
`Xvfb :99` + `ffmpeg -f x11grab` as described in the appendix. This was also verified
to work but is harder to automate reliably.

---

## Step-by-step plan (VHS / terminal approach)

### 1. Prerequisites

```bash
# Build the binary (once)
CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .

# Ensure VHS + ttyd are available
export PATH="$HOME/go/bin:$PATH"
vhs --version   # v0.11.0
ttyd --version  # 1.7.4
```

### 2. Create seed database

Run the seed script (see `scripts/demo-seed-db.py` — to be created) which inserts realistic
`/v1/chat/completions` log rows with varied models, status codes, and SSE response bodies:

```bash
python3 scripts/demo-seed-db.py --out demos/demo.db --rows 12
```

The script should insert:
- Several successful 200 streaming responses (gpt-4o, gpt-4o-mini, claude-3-5-sonnet)
- One 401 (bad auth)
- One 500 (upstream error)
- A few with reasoning/thinking blocks (for `v` stream-view toggle)

### 3. Start the management server

```bash
./demos/memoryelaine serve --config demos/demo-config.yaml &
# demo-config.yaml points to demos/demo.db, listens on 127.0.0.1:18677
```

### 4. Write the VHS tape file (`demos/demo-emacs.tape`)

```tape
Output demos/demo-emacs.gif
Set Shell "bash"
Set FontSize 14
Set Width 220
Set Height 50
Set Theme "Monokai"
Set TypingSpeed 60ms

# Launch Emacs in terminal mode with the package pre-loaded
Type `EMACS_BIN=/opt-3/emacs-30-lucid/bin/emacs`
Enter
Type `$EMACS_BIN -nw -L ./emacs-memoryelaine \`
Enter
Type `  --eval '(setq memoryelaine-base-url "http://127.0.0.1:18677" memoryelaine-username "admin" memoryelaine-password "changeme")'`
Enter
Sleep 3s

# Open the memoryelaine search buffer
Ctrl+Alt+x
Type "memoryelaine"
Enter
Sleep 2s

# Navigate down to a row
Down
Down
Down
Sleep 500ms

# Open detail view
Enter
Sleep 1500ms

# Toggle stream view (Raw <-> Assembled)
Type "v"
Sleep 1000ms

# Go back to list
Escape
Sleep 500ms

# Type a query into the filter
Type "/"
Type "status:200"
Enter
Sleep 1500ms

# Quit
Ctrl+x
Ctrl+c
```

> **Note on M-x in VHS:** VHS sends `Alt+x` for `M-x`. Use `Ctrl+Alt+x` if needed or
> pre-load the function via `--eval '(memoryelaine)'` to skip the minibuffer step.

### 5. Run the tape

```bash
cd /work
VHS_NO_SANDBOX=1 vhs demos/demo-emacs.tape
```

Output: `demos/demo-emacs.gif` (1200×600 or similar).

### 6. (Optional) Convert to MP4 for smaller file size

```bash
ffmpeg -i demos/demo-emacs.gif -movflags faststart \
       -pix_fmt yuv420p -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" \
       demos/demo-emacs.mp4
```

---

## Appendix: GUI approach (Xvfb + ffmpeg)

If a GUI Emacs window with fonts/themes is preferred:

```bash
# 1. Start virtual display
Xvfb :99 -screen 0 1280x800x24 &

# 2. Start ffmpeg capture (background)
DISPLAY=:99 ffmpeg -f x11grab -r 25 -s 1280x800 -i :99 \
  -c:v libx264 -pix_fmt yuv420p demos/demo-emacs-gui.mp4 &
FFMPEG_PID=$!

# 3. Launch Emacs GUI
DISPLAY=:99 /opt-3/emacs-30-lucid/bin/emacs \
  -L ./emacs-memoryelaine \
  --eval '(setq memoryelaine-base-url "http://127.0.0.1:18677"
               memoryelaine-username "admin"
               memoryelaine-password "changeme")' &
EMACS_PID=$!
sleep 4

# 4. Automate with xdotool
DISPLAY=:99 xdotool search --name "Emacs" --sync
DISPLAY=:99 xdotool key --delay 100 alt+x
DISPLAY=:99 xdotool type --delay 80 "memoryelaine"
DISPLAY=:99 xdotool key Return
sleep 2
DISPLAY=:99 xdotool key Down Down Down Return
sleep 2
DISPLAY=:99 xdotool key v         # toggle stream view
sleep 1
DISPLAY=:99 xdotool key Escape
sleep 1

# 5. Stop capture
kill $FFMPEG_PID
kill $EMACS_PID

# 6. Crop/trim if needed
ffmpeg -i demos/demo-emacs-gui.mp4 -ss 1 -t 20 demos/demo-emacs-final.mp4
```

### Challenges with the GUI approach

- xdotool timing is fragile — network calls to the management server add latency
- Emacs frame size/font rendering depends on available fonts in the container
- The virtual display is black unless a window manager is running (no window decorations)

---

## Expected output

A ~15–20 second GIF/MP4 showing:
1. Emacs opens with the `*memoryelaine-search*` buffer listing 10–12 log entries
2. User navigates to a row and opens detail view
3. Stream view toggles between Raw SSE and Assembled content
4. User types a query (`status:200`) to filter the list
