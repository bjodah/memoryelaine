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

# Open detail view for selected entry (row 3: quantum entanglement — has SSE body)
Enter
Sleep 2s

# Scroll detail view down
Down
Sleep 200ms
Down
Sleep 200ms
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

# Close detail view — back to table
Escape
Sleep 700ms

# Apply status filter: all → 200
Type "f"
Sleep 1000ms

# all → 200 → 400
Type "f"
Sleep 1000ms

# 400 → 500
Type "f"
Sleep 1000ms

# 500 → all (reset)
Type "f"
Sleep 700ms

# Refresh
Type "r"
Sleep 800ms

# Quit
Type "q"
Sleep 500ms
