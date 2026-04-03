# Wishlist

Things to implement.

## Web UI:

### main view
Add keyboard short cuts (and a discrete row-selection indicator on the left border of the table):
- j/k : down/up (moves selection between ID rows)
- enter : open pop-up window of selected row (same as click)
- '/' : put cursor in "Query" text box.
- 'R' : toggle recording
- 'h' : pop up describing all keyboard short cuts *and* the query language with examples

in the bottom left of the page add weak (gray?) text saying "press 'h' to show help"


### "entry view popup window"

Now that ellipsis compaction is available, the default should probably be to load bodies (unless they are very large, say 128 kiB?).

Add copy buttons for respective body.

Add keyboard short-cuts:

- Bind "ESC" *and* "u" to close (u is borrowing from gmail key shortcuts). (right now user needs to click "X" top right, or click outside popup window).
- Take inspiration from the Emacs keybindings for the entry view, and create similar bindings for the web ui.

If we now add copy buttons and keybindings for copying raw body contents (or assembled it assembled view is enabled), we can afford to pretty print the bodies in the event that they are parseable as json.
