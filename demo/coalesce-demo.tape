# Record with: vhs demo/coalesce-demo.tape  (from the repo root, heavyq on PATH)
Output docs/coalesce.gif
Set Shell bash
Set FontSize 15
Set Width 1180
Set Height 620
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

Hide
Type "source demo/setup-coalesce-demo.sh" Enter
Sleep 2s
Show

Type "# four agents finish a change on the same commit and run the same gate" Enter
Type "for a in api web worker docs; do heavyq 4 --cache --lane $a -- npm test > $a.log 2>&1 & done" Sleep 300ms Enter
Sleep 1.5s
Type "heavyq status | head -8" Sleep 300ms Enter
Sleep 4s
Type "wait; echo npm test ran $(wc -l < $DEMO_RUNS) time for 4 agents; cat docs.log" Sleep 300ms Enter
Sleep 5s
Type "heavyq 4 --cache --lane review -- npm test   # later, same commit" Sleep 300ms Enter
Sleep 3s
Type "heavyq history" Sleep 300ms Enter
Sleep 5s
