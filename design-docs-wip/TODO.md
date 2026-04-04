# WebUI

## Main view

Bottom left ghost text with "press h to show help":

- change it so it is not fixed in the viewport while scrolling
- make it normal page-flow text near the bottom of the page
- update the shortcut text to `press ? to show help`

## Detail view

- Change `n` / `p` keybinding to map to next / previous log entry instead of
  scrolling, including across filtered page boundaries
- Bind `?` to show the help popup here too
- When help is opened from detail view, limit it to only "Detail View"
  and hide "Query DSL"
- Do not add a replacement keyboard scrolling shortcut
