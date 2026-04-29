# PLAN-03-DEMO-TUI: Recording a Terminal UI Demo

## Goal

Produce an animated GIF of the `memoryelaine` interactive TUI — showing the log table,
navigating rows, opening the detail view, toggling stream view modes, paginating, and
using the status filter.

---

## Verified environment facts

| Item | Status |
|------|--------|
| VHS `v0.11.0` | ✅ `~/go/bin/vhs` (installed via `go install`) |
| ttyd `v1.7.7` | ✅ `/opt-3/ttyd/bin/ttyd` (not at `/usr/local/bin` — must export PATH) |
| `VHS_NO_SANDBOX=1` workaround | ✅ required as root |
| `memoryelaine tui` command | ✅ implemented via charmbracelet/bubbletea |
| GIF output from VHS | ✅ verified — `demos/demo-tui.gif` (469 KB, 1600×700, 526 frames) |
| MP4 output | ✅ verified — `demos/demo-tui.mp4` (251 KB) |
| Key injection via VHS | ✅ `j`/`k`/`v`/`f`/`q`/`Enter`/`Escape` all work — bubbletea receives them correctly |

## ✅ Recording complete

Output files produced:
- `demos/demo-tui.gif` — 469 KB, 1600×700 px, ~21s, 526 frames
- `demos/demo-tui.mp4` — 251 KB, H.264/yuv420p

---

## Recommended approach: VHS tape file

VHS is the ideal tool here — it drives a real `ttyd`-backed terminal, captures key presses
at scripted timing, and outputs a polished GIF. The TUI is a fullscreen charmbracelet/bubbletea
app, which renders perfectly in a terminal emulator.

---

## Step-by-step plan

### 1. Prerequisites

```bash
export PATH="$HOME/go/bin:$PATH"
vhs --version   # v0.11.0
ttyd --version  # 1.7.4

# Build binary and seed data (shared infrastructure)
CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .
python3 scripts/demo-seed-db.py --out demos/demo.db --rows 12
```

### 2. Start the management server (TUI reads from same DB)

```bash
export PATH="$HOME/go/bin:/opt-3/ttyd/bin:$PATH"
./demos/memoryelaine serve --config demos/demo-config.yaml &
sleep 2
```

The TUI connects directly to the database file — it does **not** go through the HTTP
management server — so the server only needs to be running if you want live updates
during the demo. For a purely static demo of the stored logs, the server is optional.

### 3. Write the VHS tape (`demos/demo-tui.tape`)

> **Key learnings from recording:**
> - `Set Width` and `Set Height` are in **pixels**, not terminal columns/rows
> - Minimum pixel dimension: **120×120** (enforced by VHS validation)
> - Pagination keys `n`/`p` are no-ops with 12 seed rows (limit=50); removed from tape
> - Status filter cycles exactly: nil → 200 → 400 → 500 → nil (exact match, not 4xx/5xx)
>   so seed data must include rows with `status_code = 400` and `status_code = 500` exactly
> - The `v` stream-view toggle requires a row with `AssembledAvailable=true` — requires a
>   proper SSE `resp_body`; row 3 (quantum, gpt-4o, status 200) has this in the seed data
> - ttyd is at `/opt-3/ttyd/bin/ttyd`, must be on PATH

```tape
Output demos/demo-tui.gif
Set Shell "bash"
Set FontSize 13
Set Width 1600
Set Height 700
Set Theme "Dracula"
Set TypingSpeed 80ms

# Launch the TUI against the demo database
Type "./demos/memoryelaine tui --config demos/demo-config.yaml"
Enter
Sleep 3s

# Navigate: move down several rows
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 400ms

# Open detail view for selected entry (row 3: quantum — has SSE resp body)
Enter
Sleep 2s

# Scroll detail view down
Down
Down
Down
Sleep 300ms

# Toggle stream view: Raw → Assembled
Type "v"
Sleep 1500ms

# Scroll assembled content
Down
Down
Sleep 500ms

# Toggle back to Raw
Type "v"
Sleep 1000ms

# Close detail view
Escape
Sleep 700ms

# Apply status filter (cycle: all → 200 → 400 → 500 → all)
Type "f"
Sleep 1000ms
Type "f"
Sleep 1000ms
Type "f"
Sleep 1000ms
Type "f"
Sleep 700ms

# Refresh
Type "r"
Sleep 800ms

# Quit
Type "q"
Sleep 500ms
```

### 4. Run the tape

```bash
cd /work
export PATH="$HOME/go/bin:/opt-3/ttyd/bin:$PATH"
VHS_NO_SANDBOX=1 vhs demos/demo-tui.tape
```

Output: `demos/demo-tui.gif`

### 5. Convert to MP4

```bash
ffmpeg -y -i demos/demo-tui.gif \
  -movflags faststart -pix_fmt yuv420p \
  -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" \
  demos/demo-tui.mp4
```

---

## Tape tuning tips

### Terminal dimensions

`Set Width` / `Set Height` in VHS 0.11.0 are **pixel** dimensions, not character columns/rows.
Width=1600, Height=700 at FontSize=13 gives approximately 195 columns × 40 rows — enough
for the TUI table with all columns visible.

### Timing

- After `Enter` to launch the TUI: wait **3s** for startup and initial DB query
- After `Enter` to open detail: wait **2s** for body fetch and render
- Status filter cycles need ~800ms between presses for the redraw to be visible
- Quit: `q` exits immediately; add `Sleep 500ms` after for a clean final frame

### Stream-view demo content

To show the `v` toggle meaningfully, ensure at least one seed entry has a proper SSE
response body (`data: {...}\n\ndata: [DONE]\n\n` format). The seed script should create
a streaming `gpt-4o` response so "Assembled" mode shows rendered content vs raw SSE frames.

### Export demo (optional addition)

To show the export feature (`x` then `b`/`c`):

```tape
# In detail view:
Type "x"
Sleep 300ms
Type "c"       # export assembled content
# TUI will prompt for save path
Type "/tmp/demo-export.txt"
Enter
Sleep 1s
Escape
```

---

## Scene outline (approximately 25 seconds)

| # | Keys / action | What's shown |
|---|---------------|--------------|
| 1 | Launch | TUI starts, table with 12 entries, columns: ID/Time/Method/Path/Status/Duration |
| 2 | `↓ ↓ ↓` | Row selection highlight moves |
| 3 | `Enter` | Detail panel: metadata header + response body preview |
| 4 | `↓ ↓ ↓` | Scrolling through response body |
| 5 | `v` | Stream view switches to Assembled (rendered text) |
| 6 | `v` | Switches back to Raw SSE |
| 7 | `Esc` | Returns to main table |
| 8 | `f f f f` | Cycles status filter: none → 200 → 400 → 500 → none |
| 9 | `n` / `p` | Next/previous page |
| 10 | `q` | Quit |
