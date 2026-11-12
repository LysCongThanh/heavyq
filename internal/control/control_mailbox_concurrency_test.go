package control

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// Helper-process pattern: the test binary re-executes itself as a writer that
// stands in for the dashboard or a `heavyq reprio|cancel` process. For each
// "go" line on stdin it sends one burst of requests and prints "OK" or
// "ERR <error>"; it exits when stdin closes.
const writerEnv = "HEAVYQ_CONTROL_TEST_WRITER"

func TestMain(m *testing.M) {
	if dir := os.Getenv(writerEnv); dir != "" {
		os.Exit(runWriter(dir, os.Args[len(os.Args)-2:]))
	}
	os.Exit(m.Run())
}

// runWriter args: <requests> <last prio>. A burst is <requests>-1 priority
// requests of 5 (with a cancel each), then <last prio> as its final word.
func runWriter(dir string, args []string) int {
	n, _ := strconv.Atoi(args[0])
	last, _ := strconv.Atoi(args[1])
	in := bufio.NewReader(os.Stdin)
	m := Mailbox{Dir: dir}
	for {
		if _, err := in.ReadString('\n'); err != nil { // barrier, or the parent is done
			return 0
		}
		fmt.Println(burst(m, n, last))
	}
}

func burst(m Mailbox, n, last int) string {
	if os.Getenv(targetWriterEnv) != "" {
		return targetBurst(m, n)
	}
	for i := 0; i < n; i++ {
		p := 5
		if i == n-1 {
			p = last
		}
		if err := m.RequestPrio(tk, p); err != nil {
			return fmt.Sprint("ERR ", err)
		}
		if err := m.RequestCancel(tk); err != nil {
			return fmt.Sprint("ERR ", err)
		}
	}
	return "OK"
}

type writer struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func startWriter(t *testing.T, dir string, requests, last int, env ...string) writer {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$", strconv.Itoa(requests), strconv.Itoa(last))
	cmd.Env = append(append(os.Environ(), writerEnv+"="+dir), env...)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w := writer{cmd: cmd, in: in, out: bufio.NewReader(out)}
	t.Cleanup(func() { _ = w.in.Close(); _ = w.cmd.Wait() })
	return w
}

func (w writer) goBurst(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(w.in, "go\n"); err != nil {
		t.Fatal(err)
	}
}

func (w writer) result(t *testing.T) string {
	t.Helper()
	line, err := w.out.ReadString('\n')
	if err != nil {
		t.Fatalf("writer process: %v", err)
	}
	return strings.TrimSpace(line)
}

// Two operators (two tabs, the dashboard and `heavyq reprio`) may write to one
// ticket at the same moment. Every writer must be told the truth: a request
// that is stored is not reported as failed.
func TestConcurrentWritersToOneTicketAllSucceed(t *testing.T) {
	m := Mailbox{Dir: t.TempDir() + "/ctl"}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := make([]writer, 4)
	for i := range ws {
		ws[i] = startWriter(t, m.Dir, 300, 2)
	}
	for _, w := range ws {
		w.goBurst(t)
	}
	for i, w := range ws {
		if got := w.result(t); got != "OK" {
			t.Errorf("writer %d: %s", i, got)
		}
	}
	if p, ok := m.TakePrio(tk); !ok || p != 2 {
		t.Fatalf("after the writers, TakePrio = %d %v, want 2 true", p, ok)
	}
	if !m.CancelRequested(tk) {
		t.Fatal("the cancel request is lost")
	}
	if left := strayFiles(t, m.Dir); len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

// The owner polls TakePrio while an operator keeps stepping the priority. The
// operator's last request must reach the owner: consumed by a poll, or still
// pending for the next one. Never deleted unseen.
func TestTakePrioNeverDropsTheLatestRequest(t *testing.T) {
	m := Mailbox{Dir: t.TempDir() + "/ctl"}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	w := startWriter(t, m.Dir, 20, 9)
	for round := 0; round < 200; round++ { // the old read-then-remove lost one within ~80 rounds
		done := make(chan struct{})
		seen := make(chan int, 1)
		go func() { // the owner's poll loop
			last := 0
			for {
				select {
				case <-done:
					seen <- last
					return
				default:
				}
				if p, ok := m.TakePrio(tk); ok {
					last = p
				}
			}
		}()
		w.goBurst(t)
		got := w.result(t)
		close(done)
		last := <-seen
		if got != "OK" {
			t.Fatalf("round %d: writer: %s", round, got)
		}
		if p, ok := m.TakePrio(tk); ok {
			last = p
		}
		if last != 9 {
			t.Fatalf("round %d: the owner last saw priority %d, want the final request 9", round, last)
		}
	}
	if left := strayFiles(t, m.Dir); len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

// strayFiles lists the hidden (temporary) files left in dir.
func strayFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}
