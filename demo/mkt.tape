# demo/mkt.tape — VHS script for the mkt dashboard demo GIF.
#
# Regenerate:  task demo      (builds ./mkt, then renders demo/mkt.gif)
#         or:  vhs demo/mkt.tape
#
# VHS drives the real binary through simulated keystrokes — no cooperation
# from mkt needed. Live data means frames vary run to run, which is fine
# for a demo. Uses your ~/.config/mkt watchlist, so populate one first.

Output demo/mkt.gif

Set Shell "bash"
Set FontSize 13
Set Width 1280
Set Height 720
Set Padding 16
Set Framerate 24
Set Theme "Dracula"

# Launch hidden; reveal once providers have connected and quotes land.
Hide
Type "./mkt"
Enter
Sleep 3s
Show

# Watch tab — cycle the seeded thematic groups ( [ / ] ).
Sleep 1.5s
Type "]"
Sleep 1.5s
Type "]"
Sleep 1.5s
Type "]"
Sleep 1.5s

Type "2"
Sleep 2.5s
Type "3"
Sleep 2.5s
Type "4"
Sleep 2s
Type "5"
Sleep 2s
Type "6"
Sleep 2.5s
Type "7"
Sleep 2s
Type "1"
Sleep 1s

# Theme system — T rebuilds every style in place, no restart.
Type "T"
Sleep 1.5s
Type "T"
Sleep 1.5s

# Per-tab keybinding reference.
Type "?"
Sleep 2.5s
Escape
Sleep 500ms

Type "q"
Sleep 1s
