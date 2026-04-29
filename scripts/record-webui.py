#!/usr/bin/env python3
"""Record the memoryelaine Web UI demo using Playwright."""
import subprocess, time, sys
from pathlib import Path
from playwright.sync_api import sync_playwright

VIDEO_DIR = Path("demos/webui-raw")
OUTPUT_MP4 = Path("demos/demo-webui.mp4")
OUTPUT_GIF = Path("demos/demo-webui.gif")
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
        http_credentials={"username": "demo", "password": "demo1234"},
        viewport={"width": 1280, "height": 800},
    )
    page = ctx.new_page()

    # --- Scene 1: Load main log table (12 entries) ---
    print("Scene 1: Loading page...")
    page.goto(f"{BASE_URL}/")
    page.wait_for_load_state("networkidle")
    # Wait for rows to appear in the table
    page.wait_for_selector("#log-body tr[data-log-id]", timeout=8000)
    time.sleep(2.0)

    # --- Scene 2: Show help overlay ---
    print("Scene 2: Help overlay...")
    page.keyboard.press("?")
    time.sleep(2.0)
    page.keyboard.press("Escape")
    time.sleep(0.8)

    # --- Scene 3: Navigate down 3 rows (to entry #10, "List 5 sorting algorithms") ---
    print("Scene 3: Navigating rows...")
    page.keyboard.press("j")
    time.sleep(0.35)
    page.keyboard.press("j")
    time.sleep(0.35)
    page.keyboard.press("j")
    time.sleep(0.7)

    # --- Scene 4: Open detail panel ---
    print("Scene 4: Opening detail panel...")
    page.keyboard.press("Enter")
    page.wait_for_selector("#detail-overlay:not(.hidden)", timeout=6000)
    time.sleep(2.5)

    # --- Scene 5: Conversation view (c key — always works) ---
    print("Scene 5: Conversation view...")
    page.keyboard.press("c")
    time.sleep(2.0)

    # --- Scene 6: Close detail panel ---
    print("Scene 6: Closing detail...")
    page.keyboard.press("Escape")
    time.sleep(0.8)

    # --- Scene 7: Filter with query DSL — type "quantum" ---
    print("Scene 7: Searching for 'quantum'...")
    page.keyboard.press("/")
    time.sleep(0.4)
    page.keyboard.type("quantum", delay=80)
    time.sleep(0.5)
    page.keyboard.press("Enter")
    page.wait_for_load_state("networkidle")
    time.sleep(1.5)
    # Blur the query input so keyboard nav (j/Enter) works
    page.keyboard.press("Escape")
    time.sleep(0.5)

    # --- Scene 8: Navigate into the quantum result ---
    print("Scene 8: Opening quantum entry...")
    page.keyboard.press("j")
    time.sleep(0.5)
    page.keyboard.press("Enter")
    page.wait_for_selector("#detail-overlay:not(.hidden)", timeout=6000)
    time.sleep(2.2)
    page.keyboard.press("Escape")
    time.sleep(0.8)

    # --- Scene 9: Change filter to "is:error" to show error entries ---
    print("Scene 9: is:error filter...")
    page.keyboard.press("/")
    time.sleep(0.3)
    page.keyboard.press("Control+a")
    time.sleep(0.1)
    page.keyboard.type("is:error", delay=80)
    time.sleep(0.5)
    page.keyboard.press("Enter")
    page.wait_for_load_state("networkidle")
    time.sleep(1.5)
    page.keyboard.press("Escape")
    time.sleep(0.5)

    # --- Scene 10: Toggle recording OFF then ON ---
    print("Scene 10: Toggle recording...")
    page.keyboard.press("R")
    time.sleep(1.2)
    page.keyboard.press("R")
    time.sleep(1.2)

    print("Closing browser...")
    ctx.close()
    browser.close()

# Find the .webm and convert to MP4
webm_files = sorted(VIDEO_DIR.glob("*.webm"))
if not webm_files:
    print("ERROR: No webm recorded!", file=sys.stderr)
    sys.exit(1)
webm = webm_files[-1]
print(f"Converting {webm} → {OUTPUT_MP4}")

subprocess.run([
    "ffmpeg", "-y", "-i", str(webm),
    "-c:v", "libx264", "-pix_fmt", "yuv420p",
    "-movflags", "faststart",
    str(OUTPUT_MP4)
], check=True)
print(f"Created: {OUTPUT_MP4} ({OUTPUT_MP4.stat().st_size // 1024} KB)")

# Convert MP4 → GIF (960px wide, 10fps, two-pass palette)
print(f"Converting {OUTPUT_MP4} → {OUTPUT_GIF}")
subprocess.run([
    "ffmpeg", "-y", "-i", str(OUTPUT_MP4),
    "-vf", "fps=10,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=128[p];[s1][p]paletteuse",
    str(OUTPUT_GIF)
], check=True)
print(f"Created: {OUTPUT_GIF} ({OUTPUT_GIF.stat().st_size // 1024} KB)")
