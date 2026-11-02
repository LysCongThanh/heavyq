//go:build linux

package term

import (
	"bytes"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// pty is a pseudo-terminal pair made with plain syscalls (no x/sys): the
// program under test gets slave as its terminal, the test reads master as
// the screen.
type pty struct {
	master, slave *os.File
	mu            sync.Mutex
	out           bytes.Buffer
}

func ioctl(fd uintptr, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

func newPty(t *testing.T) *pty {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	var unlock, n uint32
	if err := ioctl(m.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		t.Skipf("unlockpt: %v", err)
	}
	if err := ioctl(m.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		t.Skipf("ptsname: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open pts: %v", err)
	}
	p := &pty{master: m, slave: s}
	go func() {
		buf := make([]byte, 4096)
		for {
			k, err := m.Read(buf)
			p.mu.Lock()
			p.out.Write(buf[:k])
			p.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { s.Close(); m.Close() })
	return p
}

func (p *pty) screen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

// waitFor polls the screen until it contains want.
func (p *pty) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains([]byte(p.screen()), []byte(want)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q; got %q", want, p.screen())
}

func (p *pty) termios(t *testing.T) syscall.Termios {
	t.Helper()
	var tio syscall.Termios
	if err := ioctl(p.slave.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&tio))); err != nil {
		t.Fatal(err)
	}
	return tio
}

func (p *pty) setSize(t *testing.T, cols, rows uint16) {
	t.Helper()
	ws := [4]uint16{rows, cols, 0, 0}
	if err := ioctl(p.master.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		t.Fatal(err)
	}
}

func isRaw(tio syscall.Termios) bool {
	return tio.Lflag&(syscall.ECHO|syscall.ICANON) == 0
}

func TestStartEntersAndRestoreLeaves(t *testing.T) {
	p := newPty(t)
	before := p.termios(t)
	if isRaw(before) {
		t.Fatal("a fresh pty is cooked; fixture is wrong")
	}
	s, err := Start(p.slave, p.slave)
	if err != nil {
		t.Fatal(err)
	}
	p.waitFor(t, "\x1b[?1049h")
	p.waitFor(t, "\x1b[?1000l") // the reset at start switches mouse reporting off
	if !isRaw(p.termios(t)) {
		t.Fatal("not in raw mode after Start")
	}
	if _, err := s.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	p.waitFor(t, "frame")

	s.Restore()
	p.waitFor(t, "\x1b[?1049l")
	p.waitFor(t, "\x1b[?25h")
	if after := p.termios(t); after != before {
		t.Fatalf("termios not restored:\n before %+v\n after  %+v", before, after)
	}
	s.Restore() // idempotent
	n := len(p.screen())
	_, _ = s.Write([]byte("late"))
	time.Sleep(50 * time.Millisecond)
	if len(p.screen()) != n {
		t.Fatal("a write after Restore reached the screen")
	}
}

func TestPanicStillRestores(t *testing.T) {
	p := newPty(t)
	before := p.termios(t)
	func() {
		defer func() { _ = recover() }()
		s, err := Start(p.slave, p.slave)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Restore()
		panic("boom")
	}()
	p.waitFor(t, "\x1b[?1049l")
	if after := p.termios(t); after != before {
		t.Fatal("termios not restored after a panic")
	}
}

func TestTerminationSignalsBecomeQuitEvents(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT} {
		p := newPty(t)
		s, err := Start(p.slave, p.slave)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		select {
		case e := <-s.Events():
			if e.Kind != EvQuit || e.Sig == "" {
				t.Fatalf("%v: event %+v, want a quit", sig, e)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%v: no quit event", sig)
		}
		s.Restore()
	}
}

func TestResizeIsDebouncedAndSanityChecked(t *testing.T) {
	p := newPty(t)
	p.setSize(t, 80, 24)
	s, err := Start(p.slave, p.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Restore()
	if got := s.Size(); got != (Size{80, 24}) {
		t.Fatalf("Size = %+v, want 80x24", got)
	}
	p.setSize(t, 132, 43)
	for i := 0; i < 6; i++ { // a window being dragged
		_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case e := <-s.Events():
		if e.Kind != EvResize || e.Size != (Size{132, 43}) {
			t.Fatalf("event %+v, want resize 132x43", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no resize event")
	}
	select {
	case e := <-s.Events():
		t.Fatalf("a burst of SIGWINCH made more than one event: %+v", e)
	case <-time.After(250 * time.Millisecond):
	}

	p.setSize(t, 0, 0) // a glitch: ignored, the last sane size stays
	t.Setenv("COLUMNS", "")
	t.Setenv("LINES", "")
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	select {
	case e := <-s.Events():
		t.Fatalf("a 0x0 report produced an event: %+v", e)
	case <-time.After(250 * time.Millisecond):
	}
	if got := s.Size(); got != (Size{132, 43}) {
		t.Fatalf("Size after a 0x0 glitch = %+v, want the last sane 132x43", got)
	}
}

// Ctrl-Z itself stops the test process, so the stop is not exercised here;
// what runs after SIGCONT is: the terminal is taken again and a repaint asked for.
func TestResumeRetakesTheTerminal(t *testing.T) {
	p := newPty(t)
	s, err := Start(p.slave, p.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Restore()
	s.leave() // what Suspend does before it stops
	if isRaw(p.termios(t)) {
		t.Fatal("leave did not restore the terminal")
	}
	_, _ = s.Write([]byte("while-stopped"))
	_ = syscall.Kill(os.Getpid(), syscall.SIGCONT)
	select {
	case e := <-s.Events():
		if e.Kind != EvResume {
			t.Fatalf("event %+v, want resume", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no resume event after SIGCONT")
	}
	if !isRaw(p.termios(t)) {
		t.Fatal("raw mode not re-entered after SIGCONT")
	}
	if bytes.Contains([]byte(p.screen()), []byte("while-stopped")) {
		t.Fatal("a write while suspended reached the screen")
	}
}
