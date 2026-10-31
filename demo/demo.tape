# Record with: vhs demo/demo.tape  (from the repo root, heavyq on PATH)
Output docs/demo.gif
Set Shell bash
Set FontSize 15
Set Width 1180
Set Height 640
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type "source demo/setup-demo.sh" Enter
Sleep 6s
Show

Type "heavyq status" Sleep 300ms Enter
Sleep 6s
Type "heavyq 1 --lane release --max-wait 4 -- npm test; echo exit=$?" Sleep 300ms Enter
Sleep 6s
Hide
Type "kill $(jobs -p) 2>/dev/null; clear" Enter
Sleep 1s
