# PLAN-04-DEMO-CLI: Recording a CLI Demo

## Goal

Produce an animated GIF showing the `memoryelaine` CLI — the `log` subcommand with
various flags, format options, and the query DSL — as a quick "show it off in the terminal"
demo.

---

## Verified environment facts

| Item | Status |
|------|--------|
| VHS `v0.11.0` | ✅ `~/go/bin/vhs` (installed via `go install`) |
| ttyd `v1.7.7` | ✅ `/opt-3/ttyd/bin/ttyd` (not at `/usr/local/bin` — must export PATH) |
| `VHS_NO_SANDBOX=1` workaround | ✅ required as root |
| Binary (`memoryelaine log`) | ✅ tested with seed DB — table output works |
| GIF output | ✅ verified working — `demos/demo-cli.gif` (903 KB, 1200×600, 768 frames) |
| MP4 output | ✅ verified working — `demos/demo-cli.mp4` (553 KB) |
| `memoryelaine log -f table -n 5` | ✅ outputs 5 rows in ASCII table |
| FTS search (`-q "quantum"`) | ✅ returns 2 matching rows as expected |

**Sample verified output:**
```
ID  TIME      METHOD  PATH                  STATUS  DURATION  REQ SIZE  RESP SIZE
12  11:27:34  POST    /v1/chat/completions  200     1102ms    122       547
11  11:22:34  POST    /v1/chat/completions  200     156ms      98       256
...
```

## ✅ Recording complete

Output files produced:
- `demos/demo-cli.gif` — 903 KB, 1200×600 px, ~30s, 768 frames
- `demos/demo-cli.mp4` — 553 KB, H.264/yuv420p

---

## Recommended approach: VHS tape file

The CLI is the simplest demo to record. VHS types commands into a styled terminal and
captures the output as a GIF — no servers, no GUI, no fragility.

---

## Step-by-step plan

### 1. Prerequisites

```bash
export PATH="$HOME/go/bin:/opt-3/ttyd/bin:$PATH"
vhs --version   # v0.11.0

CGO_ENABLED=1 go build -tags sqlite_fts5 -o ./demos/memoryelaine .
python3 scripts/demo-seed-db.py --out demos/demo.db
```

No server needs to be running — the `log` command reads the SQLite DB directly.

### 2. Write the VHS tape (`demos/demo-cli.tape`)

> **Key learnings from recording:**
> - `Set Width` and `Set Height` are in **pixels**, not terminal columns/rows
> - Minimum pixel dimension: **120×120** (enforced by VHS validation)
> - Use `Set Theme "Monokai Remastered"` — `"Monokai"` is not a valid VHS theme name
> - Use non-default config credentials to suppress the `slog.Warn` about default creds
> - The `--query "path:…"` DSL was removed from the tape since that flag doesn't exist;
>   use `--path` for path filter and `-q` for FTS

```tape
Output demos/demo-cli.gif
Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 600
Set Theme "Monokai Remastered"
Set TypingSpeed 60ms

# Set up the alias for brevity in the demo
Type "alias me=./demos/memoryelaine"
Enter
Sleep 500ms

# --- Show help ---
Type "me log --help"
Enter
Sleep 2s

# --- Table view (default, last 5) ---
Type "me log --config demos/demo-config.yaml -f table -n 5"
Enter
Sleep 1500ms

# --- Filter by status 200 ---
Type "me log --config demos/demo-config.yaml -f table --status 200 -n 5"
Enter
Sleep 1500ms

# --- Full-text search ---
Type `me log --config demos/demo-config.yaml -f table -q "quantum"`
Enter
Sleep 1500ms

# --- Single record (JSON) ---
Type "me log --config demos/demo-config.yaml -f json --id 3"
Enter
Sleep 2s

# --- JSONL output (pipeline-friendly) ---
Type "me log --config demos/demo-config.yaml -f jsonl -n 3 | head -2"
Enter
Sleep 1500ms
```

### 3. Run the tape

```bash
cd /work
export PATH="$HOME/go/bin:/opt-3/ttyd/bin:$PATH"
VHS_NO_SANDBOX=1 vhs demos/demo-cli.tape
```

Output: `demos/demo-cli.gif`

---

## Variations to consider

### Show `prune --dry-run`

```tape
Type "me prune --config demos/demo-config.yaml --keep-days 1 --dry-run"
Enter
Sleep 2s
```

This demonstrates the operational side of the tool without destructive side effects.

### Show `--since` / `--until` relative time filters

```tape
Type "me log --config demos/demo-config.yaml -f table --since 2h"
Enter
Sleep 1500ms
```

### Split into two tapes

For a focused README, consider two separate GIFs:
1. **`demo-cli-query.gif`** — DSL querying workflow (the main user-facing feature)
2. **`demo-cli-output.gif`** — Output format showcase (`table`, `json`, `jsonl`)

---

## Scene outline (approximately 20 seconds)

| # | Command | What it shows |
|---|---------|---------------|
| 1 | `me log --help` | All flags with descriptions |
| 2 | `me log -f table -n 5` | Tabular overview of recent requests |
| 3 | `me log -f table --status 200` | Status filter |
| 4 | `me log -f table --query "path:… method:POST"` | DSL query |
| 5 | `me log -f table -q "quantum"` | Full-text search |
| 6 | `me log --id 3` | Single record JSON detail |
| 7 | `me log -f jsonl -n 3 \| head -2` | JSONL for pipelines |

---

## Tips

### Terminal width

The table output uses auto-sized columns. Set `Set Width 160` in VHS so the
`REQ SIZE` / `RESP SIZE` columns don't wrap on longer paths.

### Config flag brevity

If the config file is placed at `./config.yaml` (the default lookup path), the
`--config demos/demo-config.yaml` flag can be omitted, making commands shorter
and more natural looking.

### Seed data quality

For the full-text search demo (`-q "quantum"`), the seed script must include that
word in one of the request bodies. See `scripts/demo-seed-db.py` — the "Explain
quantum entanglement" sample row was designed for exactly this.
