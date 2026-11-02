package notify

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// Channel delivers one message. Send must not block the caller: a status line
// calls it, so a desktop notifier is started detached and never waited for.
type Channel interface {
	Name() string
	Send(m Message) error
}

// Env is how the environment is read, so a test can pin it.
type Env func(key string) string

// ErrNoChannel: no channel could deliver (no desktop notifier, no terminal).
var ErrNoChannel = errors.New("notify: no channel available")

// DefaultChannels picks the channels for this machine, in the order they are
// tried (the first that delivers wins, so one event is one notice):
//
//	HEAVYQ_NOTIFY=off        none
//	HEAVYQ_NOTIFY=terminal   the terminal only
//	HEAVYQ_NOTIFY=desktop    notify-send / osascript only
//	otherwise                desktop, then the terminal
//
// Over ssh (SSH_CONNECTION set) the desktop is not the person's, so only the
// terminal is used: OSC and bell travel back through the connection.
func DefaultChannels(env Env, goos string) []Channel {
	mode := strings.ToLower(env("HEAVYQ_NOTIFY"))
	if mode == "off" {
		return nil
	}
	var chans []Channel
	if mode != "terminal" && env("SSH_CONNECTION") == "" {
		if d := desktopChannel(env, goos); d != nil {
			chans = append(chans, d)
		}
	}
	if mode != "desktop" {
		chans = append(chans, TerminalChannel{Path: "/dev/tty", Style: terminalStyle(env)})
	}
	return chans
}

// Deliver tries the channels in order and stops at the first that works. It
// returns the name of the channel that delivered.
func Deliver(chans []Channel, m Message) (string, error) {
	var errs []error
	for _, c := range chans {
		if err := c.Send(m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
			continue
		}
		return c.Name(), nil
	}
	if len(errs) == 0 {
		return "", ErrNoChannel
	}
	return "", errors.Join(errs...)
}

// DesktopChannel runs a notifier program with the message as arguments (never
// through a shell, so the text cannot become a command).
type DesktopChannel struct {
	Label string // "notify-send" or "osascript"
	Path  string // the program
	Args  func(Message) []string
}

// Name is the channel's label.
func (d DesktopChannel) Name() string { return d.Label }

// Send starts the notifier detached (own session, no stdio) and returns at
// once: a notifier with no bus to talk to can hang, and that is its problem.
func (d DesktopChannel) Send(m Message) error {
	cmd := exec.Command(d.Path, d.Args(clean(m))...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it if this process lives on (recap); a status line exits first
	return nil
}

// desktopChannel finds the notifier of this OS, nil when there is none usable.
func desktopChannel(env Env, goos string) Channel {
	switch goos {
	case "linux":
		if env("DISPLAY") == "" && env("WAYLAND_DISPLAY") == "" && env("DBUS_SESSION_BUS_ADDRESS") == "" {
			return nil // no desktop session to show it on
		}
		if p, err := exec.LookPath("notify-send"); err == nil {
			return DesktopChannel{Label: "notify-send", Path: p, Args: func(m Message) []string {
				return []string{"--app-name=heavyq", "--", m.Title, m.Body}
			}}
		}
	case "darwin":
		if p, err := exec.LookPath("osascript"); err == nil {
			// The text goes in as arguments of the script, never spliced into its source.
			return DesktopChannel{Label: "osascript", Path: p, Args: func(m Message) []string {
				return []string{"-e", "on run argv", "-e", "display notification (item 1 of argv) with title (item 2 of argv)",
					"-e", "end run", m.Body, m.Title}
			}}
		}
	}
	return nil
}

// Terminal notification styles.
const (
	styleBell   = "bell"
	styleOSC9   = "osc9"   // iTerm2, WezTerm, Ghostty, Windows Terminal
	styleOSC777 = "osc777" // foot, urxvt with the notify plugin
)

// terminalStyle picks the richest sequence the terminal is known to show. Under
// tmux or screen the sequence would need passthrough the user may not have
// enabled, so those get the bell, which always travels.
func terminalStyle(env Env) string {
	if env("TMUX") != "" || strings.HasPrefix(env("TERM"), "screen") || strings.HasPrefix(env("TERM"), "tmux") {
		return styleBell
	}
	switch p := env("TERM_PROGRAM"); {
	case p == "iTerm.app", p == "WezTerm", p == "ghostty", env("WT_SESSION") != "":
		return styleOSC9
	}
	if strings.HasPrefix(env("TERM"), "foot") {
		return styleOSC777
	}
	return styleBell
}

// TerminalChannel writes to the controlling terminal (or any file, for tests).
type TerminalChannel struct {
	Path  string    // opened for writing at Send time; "/dev/tty"
	Style string    // bell, osc9 or osc777
	Out   io.Writer // when set, written instead of opening Path
}

// Name is the channel's label.
func (TerminalChannel) Name() string { return "terminal" }

// Send writes one notification sequence. With no terminal to open it fails, so
// the next channel (or none) is tried.
func (t TerminalChannel) Send(m Message) error {
	out := t.Out
	if out == nil {
		f, err := os.OpenFile(t.Path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	m = clean(m)
	body := strings.ReplaceAll(m.Body, "\n", " | ")
	var seq string
	switch t.Style {
	case styleOSC9:
		seq = "\x1b]9;" + m.Title + ": " + body + "\x07"
	case styleOSC777:
		seq = "\x1b]777;notify;" + m.Title + ";" + body + "\x07"
	default:
		seq = "\a"
	}
	_, err := io.WriteString(out, seq)
	return err
}

// clean removes control characters from a message (ESC and BEL would end or
// forge a terminal sequence) and shortens it, except for the newlines that
// separate coalesced lines.
func clean(m Message) Message {
	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' || r >= 0x20 && r != 0x7f && (r < 0x80 || r >= 0xa0) {
				return r
			}
			return -1
		}, s)
	}
	m.Title, m.Body = strip(m.Title), strip(m.Body)
	if r := []rune(m.Body); len(r) > 400 {
		m.Body = string(r[:400]) + "..."
	}
	return m
}

// SystemEnv reads the process environment and OS.
func SystemEnv() (Env, string) { return os.Getenv, runtime.GOOS }
