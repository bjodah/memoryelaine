Output demos/demo-cli.gif
Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 600
Set Theme "Monokai Remastered"
Set TypingSpeed 60ms

# Set up alias for brevity
Type "alias me=./demos/memoryelaine"
Enter
Sleep 500ms

# --- Show help ---
Type "me log --help"
Enter
Sleep 2s

# --- Table view: last 5 entries ---
Type "me log --config demos/demo-config.yaml -f table -n 5"
Enter
Sleep 1500ms

# --- Filter by status 200 ---
Type "me log --config demos/demo-config.yaml -f table --status 200 -n 5"
Enter
Sleep 1500ms

# --- Full-text search: find the quantum entry ---
Type `me log --config demos/demo-config.yaml -f table -q "quantum"`
Enter
Sleep 1500ms

# --- Single record JSON detail ---
Type "me log --config demos/demo-config.yaml -f json --id 3"
Enter
Sleep 2s

# --- JSONL output (pipeline-friendly) ---
Type "me log --config demos/demo-config.yaml -f jsonl -n 3 | head -2"
Enter
Sleep 1500ms
