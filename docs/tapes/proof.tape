# Record with: vhs docs/tapes/proof.tape  (from the repo root; needs go, git, vhs and ttyd)
# Real output only: heavyq is built from this checkout by proof-setup.sh and runs against a tiny Go module.
Output docs/proof.gif
Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 480
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 14ms

Hide
Type "source docs/tapes/proof-setup.sh && echo setup-$((6*7))" Enter
Wait+Screen@180s /setup-42/
Type "clear" Enter
Sleep 500ms
Show

Type "# agent: all tests pass ✔" Enter
Sleep 1s
Type "echo '// tweak' >> cart.go" Enter
Type "heavyq gate status; echo exit=$?" Enter
Sleep 2s
Type "heavyq gate run" Enter
Sleep 2.2s
Type "heavyq gate status; echo exit=$?" Enter
Sleep 2.4s
Type "clear" Enter
Type "# Agents say done. heavyq knows." Enter
Type "# go install github.com/LysCongThanh/heavyq@latest" Enter
Sleep 2.4s
Hide
Type "cd / && rm -rf $(dirname $HEAVYQ_DIR)" Enter
