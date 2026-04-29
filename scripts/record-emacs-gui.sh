#!/usr/bin/env bash
# record-emacs-gui.sh — records demos/demo-emacs-gui.mp4 and .gif
# Uses Xvfb + openbox + ffmpeg (x11grab) + xdotool to automate GUI Emacs.
set -euo pipefail

DISPLAY_NUM=99
DISP=":${DISPLAY_NUM}"
EMACS=/opt-3/emacs-30-lucid/bin/emacs
LOAD_PATH=./emacs-memoryelaine
OUT_MP4=demos/demo-emacs-gui.mp4
OUT_GIF=demos/demo-emacs-gui.gif
W=1280
H=800

cleanup() {
  kill "${FFMPEG_PID:-}" 2>/dev/null || true
  wait "${FFMPEG_PID:-}" 2>/dev/null || true
  kill "${SERVER_PID:-}" 2>/dev/null || true
  kill "${OPENBOX_PID:-}" 2>/dev/null || true
  kill "${XVFB_PID:-}" 2>/dev/null || true
}
trap cleanup EXIT

# ── 1. Virtual display ────────────────────────────────────────────────────────
echo "[1/8] Starting Xvfb :${DISPLAY_NUM}..."
Xvfb :${DISPLAY_NUM} -screen 0 ${W}x${H}x24 &
XVFB_PID=$!
sleep 1

# ── 2. Window manager (required for xdotool EWMH windowfocus) ────────────────
echo "[2/8] Starting openbox..."
DISPLAY=${DISP} openbox &
OPENBOX_PID=$!
sleep 2

# ── 3. Start ffmpeg recording ─────────────────────────────────────────────────
echo "[3/8] Starting ffmpeg x11grab -> ${OUT_MP4}..."
DISPLAY=${DISP} ffmpeg -y \
  -f x11grab -r 25 -s ${W}x${H} -i :${DISPLAY_NUM} \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
  "${OUT_MP4}" &
FFMPEG_PID=$!
sleep 1

# ── 4. Start the demo management server ──────────────────────────────────────
echo "[4/8] Starting demo server..."
./demos/memoryelaine serve --config demos/demo-config.yaml \
  >/tmp/demo-emacs-gui-server.log 2>&1 &
SERVER_PID=$!
sleep 3
curl -sf http://127.0.0.1:18677/health >/dev/null \
  || { echo "ERROR: demo server did not start"; exit 1; }
echo "  Server ready (PID ${SERVER_PID})"

# ── 5. Launch Emacs GUI ───────────────────────────────────────────────────────
# Critical: unset EMACS_SOCKET — the sandbox sets it to a running daemon socket;
# without this, the Lucid binary connects to that daemon instead of creating a
# new GUI frame and then exits silently.
echo "[5/8] Launching Emacs GUI..."
setsid env -u EMACS_SOCKET DISPLAY=${DISP} \
  ${EMACS} -Q --no-desktop -geometry 140x45+0+0 \
  -L ${LOAD_PATH} \
  --eval '(progn
    (require (quote memoryelaine))
    (setq memoryelaine-base-url "http://127.0.0.1:18677"
          memoryelaine-username "demo"
          memoryelaine-password "demo1234"))' \
  >/tmp/demo-emacs-gui-emacs.log 2>&1 &
EMACS_PID=$!
sleep 10   # allow Emacs frame + fonts to settle

# ── 6. Find the main Emacs frame via xdotool ─────────────────────────────────
echo "[6/8] Finding Emacs window..."
WIN=""
for attempt in 1 2 3 4 5; do
  for wid in $(DISPLAY=${DISP} xdotool search --class "Emacs" 2>/dev/null || true); do
    name=$(DISPLAY=${DISP} xdotool getwindowname "$wid" 2>/dev/null || true)
    if [[ -n "${name}" ]]; then
      WIN="$wid"
      echo "  Window ${wid}: '${name}'"
      # prefer a frame whose name suggests the main frame
      if [[ "$name" == *"scratch"* || "$name" == *"memoryelaine"* \
            || "$name" == *"GNU Emacs"* ]]; then
        break 2
      fi
    fi
  done
  [[ -n "${WIN}" ]] && break
  echo "  Waiting for Emacs window (attempt ${attempt})..."
  sleep 3
done
echo "  Using window ID: ${WIN}"
[[ -z "${WIN}" ]] && { echo "ERROR: Emacs window not found"; exit 1; }

# ── Key-injection helpers ─────────────────────────────────────────────────────
send_key() {
  DISPLAY=${DISP} xdotool windowfocus --sync "${WIN}" 2>/dev/null || true
  sleep 0.3
  DISPLAY=${DISP} xdotool key --window "${WIN}" "$@"
}
send_type() {
  DISPLAY=${DISP} xdotool windowfocus --sync "${WIN}" 2>/dev/null || true
  sleep 0.3
  DISPLAY=${DISP} xdotool type --window "${WIN}" --delay 60 "$@"
}

# ── 7. Automation ─────────────────────────────────────────────────────────────
echo "[7/8] Automating Emacs..."

# Open the memoryelaine search buffer via M-x
send_key alt+x
sleep 0.6
send_type "memoryelaine"
sleep 0.3
send_key Return
sleep 7   # wait for async curl call + buffer render

# Navigate down to row 3 (quantum entanglement)
send_key Down
sleep 0.4
send_key Down
sleep 0.4

# Open detail view
send_key Return
sleep 5

# Scroll through response body (n = next-line in show mode)
send_key n
sleep 0.3
send_key n
sleep 0.3
send_key n
sleep 0.3

# Return to search list
send_key q
sleep 1

# Filter by keyword: s -> type "quantum" -> Return
send_key s
sleep 0.6
send_type "quantum"
sleep 0.3
send_key Return
sleep 5

# Open the single filtered result
send_key Down
sleep 0.4
send_key Return
sleep 4

# Return to search list
send_key q
sleep 0.8

# Quit Emacs (C-x C-c)
send_key ctrl+x
sleep 0.3
send_key ctrl+c
sleep 3   # allow final frames to be recorded

# ── 8. Stop recording and cleanup ────────────────────────────────────────────
echo "[8/8] Stopping recording..."
kill -INT "${FFMPEG_PID}" 2>/dev/null || true
wait "${FFMPEG_PID}" 2>/dev/null || true
FFMPEG_PID=""   # prevent double-kill in trap

kill "${SERVER_PID}" 2>/dev/null || true
SERVER_PID=""
kill "${OPENBOX_PID}" 2>/dev/null || true
OPENBOX_PID=""
kill "${XVFB_PID}" 2>/dev/null || true
XVFB_PID=""

# ── Convert MP4 → GIF (960px wide, 10fps, palette-optimised) ─────────────────
echo "Converting to GIF (960px, 10fps)..."
ffmpeg -y -i "${OUT_MP4}" \
  -vf "fps=10,scale=960:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=128[p];[s1][p]paletteuse" \
  "${OUT_GIF}"

ls -lh "${OUT_MP4}" "${OUT_GIF}"
echo "Done."
