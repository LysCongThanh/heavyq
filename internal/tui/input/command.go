package input

// Command is what a key means in the normal (not typing) mode.
type Command int

// Commands. CmdYes and CmdNo answer an inline y/n prompt; outside a prompt
// CmdYes copies the selected command (OSC 52).
const (
	CmdNone Command = iota
	CmdUp
	CmdDown
	CmdPageUp
	CmdPageDown
	CmdTop
	CmdBottom
	CmdNextSection
	CmdPrevSection
	CmdWhy     // Enter
	CmdBack    // Esc: close an overlay, clear a filter
	CmdFilter  // /
	CmdRefresh // r
	CmdHelp    // ?
	CmdQuit
	CmdSuspend // Ctrl-Z
	CmdRedraw  // Ctrl-L
	CmdMode    // m: next display mode
	CmdCancel  // c
	CmdRaise   // + : a smaller priority number runs sooner
	CmdLower   // -
	CmdPause   // p
	CmdYes
	CmdNo
)

// Map is the meaning of k in normal mode.
func Map(k Key) Command {
	switch k.Kind {
	case KeyUp:
		return CmdUp
	case KeyDown:
		return CmdDown
	case KeyPageUp:
		return CmdPageUp
	case KeyPageDown:
		return CmdPageDown
	case KeyHome:
		return CmdTop
	case KeyEnd:
		return CmdBottom
	case KeyTab:
		return CmdNextSection
	case KeyShiftTab:
		return CmdPrevSection
	case KeyEnter:
		return CmdWhy
	case KeyEsc:
		return CmdBack
	case KeyCtrlC:
		return CmdQuit
	case KeyCtrlZ:
		return CmdSuspend
	case KeyCtrlL:
		return CmdRedraw
	case KeyRune:
		return mapRune(k.Rune)
	}
	return CmdNone
}

func mapRune(r rune) Command {
	switch r {
	case 'k':
		return CmdUp
	case 'j':
		return CmdDown
	case 'g':
		return CmdTop
	case 'G':
		return CmdBottom
	case '/':
		return CmdFilter
	case 'r':
		return CmdRefresh
	case '?':
		return CmdHelp
	case 'q':
		return CmdQuit
	case 'm':
		return CmdMode
	case 'c':
		return CmdCancel
	case '+', '=':
		return CmdRaise
	case '-', '_':
		return CmdLower
	case 'p':
		return CmdPause
	case 'y', 'Y':
		return CmdYes
	case 'n', 'N':
		return CmdNo
	}
	return CmdNone
}
