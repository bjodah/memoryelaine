Output demos/demo-emacs-tui.gif
Set Shell "bash"
Set FontSize 13
Set Width 1200
Set Height 700
Set Theme "Dracula"
Set TypingSpeed 50ms

# Ensure VHS can find ttyd and the built binary
Type "export PATH=/opt-3/ttyd/bin:$PATH"
Enter
Sleep 300ms

# Start the memoryelaine server in the background
Type "./demos/memoryelaine serve --config demos/demo-config.yaml &>/tmp/demo-emacs-server.log &"
Enter
Sleep 3500ms

# Launch Emacs in terminal (-nw) mode — init file sets base-url/auth and calls (memoryelaine)
Type "/opt-3/emacs-30-lucid/bin/emacs -nw -Q -L ./emacs-memoryelaine -l ./demos/demo-emacs-init.el"
Enter
Sleep 6s

# Emacs shows the *memoryelaine* search buffer with all 12 log entries
# Move down to row 3 (quantum entanglement)
Down
Sleep 300ms
Down
Sleep 300ms

# Open the detail view (RET)
Enter
Sleep 5s

# Scroll through the response body
Down
Sleep 200ms
Down
Sleep 200ms
Down
Sleep 300ms

# Go back to the search list (q = quit-window)
Type "q"
Sleep 800ms

# Filter by keyword: press s, type "quantum", Enter
Type "s"
Sleep 600ms
Type "quantum"
Enter
Sleep 4s

# Navigate to the single result and open it
Down
Sleep 300ms
Enter
Sleep 4s

# Return to search list
Type "q"
Sleep 700ms

# Quit Emacs
Ctrl+x
Sleep 200ms
Ctrl+c
Sleep 1500ms

# Stop the background server
Type "kill $(cat /tmp/demo-emacs-server.pid 2>/dev/null) 2>/dev/null; kill %1 2>/dev/null; true"
Enter
Sleep 500ms
