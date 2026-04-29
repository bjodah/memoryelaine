# PLAN-02-DEMO-WEBUI: Recording a Web UI Demo

## Goal

Produce an animated demo (`.mp4` or `.gif`) of the `memoryelaine` management Web UI — showing
the log table, filtering with the query DSL, opening a detail panel, and toggling stream view.

---

## Verified environment facts

| Item | Status |
|------|--------|
| Playwright Python (`v1.58.0`) | ✅ installed |
| Chromium for Playwright | ✅ `~/.cache/ms-playwright/chromium-1208` |
| `--no-sandbox` flag works | ✅ verified (required when running as root) |
| `record_video_dir` produces `.webm` | ✅ verified |
| ffmpeg WebM→MP4 conversion | ✅ verified |
| Management server + seed DB | ✅ Playwright successfully loaded the WebUI with 5 rows |

**Key finding:** Playwright must connect via `127.0.0.1` (not `localhost`) — connection
refused otherwise. Use `http_credentials` context option for Basic Auth instead of URL-embedded
credentials.

---

## Recommended approach: Playwright Python script → WebM → MP4

Playwright's `record_video_dir` automatically captures every page interaction as a `.webm`.
Combined with ffmpeg for post-processing, this is the cleanest, most reliable path.

### Why prefer this over a screen-recorder approach

- Headless, no desktop/window-manager needed
- Video captures real browser rendering of the CSS/JS UI
- Interactions (clicks, typing, scrolling) are scripted precisely
- Reproducible and easy to re-run if content changes

---

## Step-by-step plan

### 1. Prerequisites

```bash
pip install playwright  # already installed
playwright install chromium  # already present

# Build binary and seed data (shared with other demos — see RECOMMENDATIONS)
CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .
python3 scripts/demo-seed-db.py --out demos/demo.db --rows 12
```

### 2. Start management server

```bash
./demos/memoryelaine serve --config demos/demo-config.yaml &
# Listens on 127.0.0.1:18677
sleep 2
curl -s http://127.0.0.1:18677/health  # verify
```

### 3. Write the Playwright recording script (`scripts/record-webui.py`)

```python
#!/usr/bin/env python3
"""Record the memoryelaine Web UI demo."""
import subprocess, time, os
from pathlib import Path
from playwright.sync_api import sync_playwright

VIDEO_DIR = Path("demos/webui-raw")
OUTPUT_MP4 = Path("demos/demo-webui.mp4")
BASE_URL = "http://127.0.0.1:18677"

VIDEO_DIR.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(
        headless=True,
        args=["--no-sandbox", "--disable-dev-shm-usage"]
    )
    ctx = browser.new_context(
        record_video_dir=str(VIDEO_DIR),
        record_video_size={"width": 1280, "height": 800},
        http_credentials={"username": "admin", "password": "changeme"},
        viewport={"width": 1280, "height": 800},
    )
    page = ctx.new_page()

    # --- Scene 1: Load main log table ---
    page.goto(f"{BASE_URL}/")
    page.wait_for_load_state("networkidle")
    time.sleep(1.5)

    # --- Scene 2: Select first row via keyboard ---
    page.keyboard.press("j")          # select row 1
    time.sleep(0.4)
    page.keyboard.press("j")          # select row 2
    time.sleep(0.4)
    page.keyboard.press("j")          # select row 3
    time.sleep(0.6)

    # --- Scene 3: Open detail panel ---
    page.keyboard.press("Enter")
    page.wait_for_selector("#detail-overlay:not(.hidden)", timeout=5000)
    time.sleep(2)

    # --- Scene 4: Toggle stream view (Raw → Assembled) ---
    page.keyboard.press("v")
    time.sleep(1.5)

    # --- Scene 5: Close detail panel ---
    page.keyboard.press("Escape")
    time.sleep(1)

    # --- Scene 6: Type a query in the search box ---
    page.keyboard.press("/")          # focus query input
    time.sleep(0.3)
    page.keyboard.type("status:200", delay=80)
    time.sleep(0.5)
    page.keyboard.press("Enter")
    page.wait_for_load_state("networkidle")
    time.sleep(2)

    # --- Scene 7: Toggle Recording OFF / ON ---
    page.keyboard.press("R")
    time.sleep(1)
    page.keyboard.press("R")
    time.sleep(1)

    ctx.close()
    browser.close()

# Find the .webm file and convert to MP4
webm_files = list(VIDEO_DIR.glob("*.webm"))
assert webm_files, "No webm recorded!"
webm = webm_files[0]

subprocess.run([
    "ffmpeg", "-y", "-i", str(webm),
    "-c:v", "libx264", "-pix_fmt", "yuv420p",
    "-movflags", "faststart",
    str(OUTPUT_MP4)
], check=True)
print(f"Created: {OUTPUT_MP4}")
```

### 4. Run

```bash
python3 scripts/record-webui.py
```

Output: `demos/demo-webui.mp4`

### 5. (Optional) Convert MP4 → GIF for embedding in Markdown

```bash
ffmpeg -i demos/demo-webui.mp4 \
  -vf "fps=12,scale=1280:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  demos/demo-webui.gif
```

Note: A full 1280×800 GIF will be large (~5–15 MB). Consider a 960×600 crop:
```bash
ffmpeg -i demos/demo-webui.mp4 \
  -vf "scale=960:-1,fps=10,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse" \
  demos/demo-webui.gif
```

---

## Scene outline (approximately 20 seconds total)

| # | What's shown | Duration |
|---|--------------|----------|
| 1 | Page loads, log table visible with 12 entries | 2 s |
| 2 | Keyboard `j` navigation highlights rows | 1.5 s |
| 3 | Enter opens detail panel, shows metadata + body preview | 3 s |
| 4 | `v` toggles Raw SSE → Assembled content | 2 s |
| 5 | Escape closes panel, back to table | 1 s |
| 6 | `/` focuses query box, type `status:200`, Enter filters | 3 s |
| 7 | `R` toggles recording indicator OFF then ON | 2 s |

---

## Troubleshooting

**`ERR_CONNECTION_REFUSED`** — Server not running or using `localhost` instead of `127.0.0.1`.

**Blank log table** — The seed DB may not have the FTS5 triggers populated. Make sure the
binary was built with `-tags sqlite_fts5` before seeding, or seed by proxying real requests
through the running server.

**Video is black** — `--disable-dev-shm-usage` is required in some environments; already
included in the script above.

**Detail panel doesn't open** — The `Enter` keypress assumes a row is already keyboard-selected
via `j`. Wait for `networkidle` after the initial page load before sending keys.
