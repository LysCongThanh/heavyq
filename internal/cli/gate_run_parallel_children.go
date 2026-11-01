package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

// childSet runs the commands of `gate run --parallel` as child heavyq
// processes, one per command, so every ticket in the queue has a pid of its
// own (`heavyq cancel <pid>` stops exactly one), its own signal handling and
// no shared stdin. The parent only starts them, prefixes and forwards their
// output, relays signals, and collects exit codes.
type childSet struct {
	exe    string
	cfg    string // absolute config path to pass on ("" = none)
	stdout io.Writer
	stderr io.Writer

	outMu sync.Mutex // one line at a time on stdout and stderr, so lines never interleave

	mu      sync.Mutex
	started []*exec.Cmd
	pending os.Signal // a signal received before every child was started
}

// childArgs is the argv (after the program) of a child: `heavyq [--config F]
// <prio> --gate ... -- sh -c <command>`, exactly what a person would type.
func (s *childSet) childArgs(runArgs []string) []string {
	if s.cfg == "" {
		return runArgs
	}
	return append([]string{"--config", s.cfg}, runArgs...)
}

// runAll starts one child per entry of argsByKey (keys are the numbers shown
// in the `[cmd N]` prefix, 1-based) and waits for all of them. It returns the
// exit code of each key: the child's own, 128+n when a signal ended it, 1 when
// it could not be started.
func (s *childSet) runAll(keys []int, runArgs map[int][]string) map[int]int {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case sig := <-sigs:
				s.forward(sig)
			case <-done:
				return
			}
		}
	}()

	rcs := make(map[int]int, len(keys))
	var rmu sync.Mutex
	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc := s.runOne(k, runArgs[k])
			rmu.Lock()
			rcs[k] = rc
			rmu.Unlock()
		}()
	}
	wg.Wait()
	return rcs
}

// forward relays sig to every running child; one started later gets it as
// soon as it exists.
func (s *childSet) forward(sig os.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = sig
	for _, c := range s.started {
		_ = c.Process.Signal(sig)
	}
}

func (s *childSet) runOne(n int, runArgs []string) int {
	cmd := exec.Command(s.exe, s.childArgs(runArgs)...)
	// Its own process group: a terminal Ctrl-C reaches only the parent, which
	// forwards it once, instead of every child getting it twice. No stdin: the
	// children must not fight over the parent's.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = nil
	out, err := cmd.StdoutPipe()
	if err != nil {
		return s.startFailed(n, err)
	}
	errp, err := cmd.StderrPipe()
	if err != nil {
		return s.startFailed(n, err)
	}
	s.mu.Lock()
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return s.startFailed(n, err)
	}
	s.started = append(s.started, cmd)
	if s.pending != nil {
		_ = cmd.Process.Signal(s.pending)
	}
	s.mu.Unlock()

	prefix := fmt.Sprintf("[cmd %d] ", n)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.copyLines(s.stdout, out, prefix) }()
	go func() { defer wg.Done(); s.copyLines(s.stderr, errp, prefix) }()
	wg.Wait() // the pipes must be drained before Wait closes them
	return processExit(cmd.Wait())
}

func (s *childSet) startFailed(n int, err error) int {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	fmt.Fprintf(s.stderr, "heavyq: [cmd %d] cannot start %s: %v\n", n, s.exe, err)
	return 1
}

// copyLines writes src to dst line by line, each line starting with prefix.
// A line is written whole under outMu, so output of two children never mixes
// inside a line; one longer than the read buffer is split across writes. A
// last line without a newline gets one.
func (s *childSet) copyLines(dst io.Writer, src io.Reader, prefix string) {
	r := bufio.NewReaderSize(src, 64<<10)
	atStart := true
	for {
		frag, more, err := r.ReadLine()
		if err == nil {
			s.outMu.Lock()
			if atStart {
				_, _ = io.WriteString(dst, prefix)
			}
			_, _ = dst.Write(frag)
			if !more {
				_, _ = io.WriteString(dst, "\n")
			}
			s.outMu.Unlock()
			atStart = !more
		}
		if err != nil {
			if !atStart { // the stream ended inside a long line
				s.outMu.Lock()
				_, _ = io.WriteString(dst, "\n")
				s.outMu.Unlock()
			}
			return
		}
	}
}
