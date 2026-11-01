package lease

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Helper-process pattern: the test binary re-executes itself. A helper
// claims once on "go", prints "<item>" or "NONE", then stays alive until the
// parent closes its stdin (or exits at once when helperExitEnv is set).
const (
	helperEnv     = "HEAVYQ_LEASE_TEST_HELPER"
	helperExitEnv = "HEAVYQ_LEASE_TEST_HELPER_EXIT" // helper exits right after claiming
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(helperEnv); dir != "" {
		os.Exit(runHelper(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runHelper args: <lane> <item>...
func runHelper(dir string, args []string) int {
	in := bufio.NewReader(os.Stdin)
	s, err := Open(dir)
	if err != nil {
		fmt.Println("ERR", err)
		return 1
	}
	o, err := NewOwner(args[0], "sess-"+args[0], 0)
	if err != nil {
		fmt.Println("ERR", err)
		return 1
	}
	if _, err := in.ReadString('\n'); err != nil { // barrier: wait for "go"
		return 1
	}
	l, ok, err := s.ClaimNext(args[1:], o)
	switch {
	case err != nil:
		fmt.Println("ERR", err)
	case !ok:
		fmt.Println("NONE")
	default:
		fmt.Println(l.Item)
	}
	if os.Getenv(helperExitEnv) != "" {
		return 0 // CLI-style: claim, print, exit at once
	}
	_, _ = io.Copy(io.Discard, in) // stay alive until the parent is done
	return 0
}

func itemNames(n int) []string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("item-%02d", i)
	}
	return items
}

func TestClaimNextGoroutinesNeverDuplicate(t *testing.T) {
	s := newStore(t)
	items := itemNames(8)
	const workers = 16
	got := make([]string, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		o := owner(t, fmt.Sprintf("lane-%d", i), "s")
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			l, ok, err := s.ClaimNext(items, o)
			errs[i] = err
			if ok {
				got[i] = l.Item
			}
		}()
	}
	close(start)
	wg.Wait()
	checkDistinct(t, got, errs, len(items))
	if ls, _ := s.List(); len(ls) != len(items) {
		t.Fatalf("leases = %d, want %d", len(ls), len(items))
	}
	evs := readEvents(t, s)
	if len(evs) != len(items) {
		t.Fatalf("events = %d, want %d", len(evs), len(items))
	}
}

func TestClaimNextProcessesNeverDuplicate(t *testing.T) {
	common := t.TempDir()
	if _, err := Open(common); err != nil {
		t.Fatal(err)
	}
	items := itemNames(5)
	const procs = 8
	type proc struct {
		cmd *exec.Cmd
		in  io.WriteCloser
		out *bufio.Reader
	}
	var ps []proc
	for i := 0; i < procs; i++ {
		cmd := exec.Command(os.Args[0], append([]string{fmt.Sprintf("p%d", i)}, items...)...)
		cmd.Env = append(os.Environ(), helperEnv+"="+common)
		in, _ := cmd.StdinPipe()
		out, _ := cmd.StdoutPipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		ps = append(ps, proc{cmd, in, bufio.NewReader(out)})
	}
	defer func() {
		for _, p := range ps {
			p.in.Close()
			_ = p.cmd.Wait()
		}
	}()
	for _, p := range ps {
		fmt.Fprintln(p.in, "go")
	}
	got := make([]string, procs)
	errs := make([]error, procs)
	for i, p := range ps {
		line, err := p.out.ReadString('\n')
		line = strings.TrimSpace(line)
		switch {
		case err != nil:
			errs[i] = fmt.Errorf("helper %d: %v", i, err)
		case strings.HasPrefix(line, "ERR"):
			errs[i] = fmt.Errorf("helper %d: %s", i, line)
		case line != "NONE":
			got[i] = line
		}
	}
	checkDistinct(t, got, errs, len(items))
}

// checkDistinct: no errors, no duplicates, and exactly min(workers, items)
// workers got an item (the rest got none).
func checkDistinct(t *testing.T, got []string, errs []error, nItems int) {
	t.Helper()
	var claimed []string
	for i, e := range errs {
		if e != nil {
			t.Errorf("worker %d: %v", i, e)
		}
		if got[i] != "" {
			claimed = append(claimed, got[i])
		}
	}
	sort.Strings(claimed)
	for i := 1; i < len(claimed); i++ {
		if claimed[i] == claimed[i-1] {
			t.Fatalf("item %s claimed twice: %v", claimed[i], claimed)
		}
	}
	want := nItems
	if len(got) < want {
		want = len(got)
	}
	if len(claimed) != want {
		t.Fatalf("claimed %d items %v, want %d", len(claimed), claimed, want)
	}
}

// A CLI-style claim: the claiming process exits immediately, as `heavyq item
// claim` does. The lease must stay valid until its TTL, not turn stale with
// the pid.
func TestClaimFromShortLivedProcessStaysValid(t *testing.T) {
	common := t.TempDir()
	s, err := Open(common)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "cli-lane", "job")
	cmd.Env = append(os.Environ(), helperEnv+"="+common, helperExitEnv+"=1")
	cmd.Stdin = strings.NewReader("go\n")
	out, err := cmd.Output() // returns once the helper has exited
	if err != nil || strings.TrimSpace(string(out)) != "job" {
		t.Fatalf("helper: %q %v", out, err)
	}
	ls, err := s.List()
	if err != nil || len(ls) != 1 || ls[0].Stale || ls[0].Pid != 0 {
		t.Fatalf("list = %+v %v", ls, err)
	}
	_, err = s.Claim("job", owner(t, "other", "s"))
	var held *HeldError
	if !errors.As(err, &held) || held.Holder.Lane != "cli-lane" {
		t.Fatalf("claim by other = %v", err)
	}
	// The same session, from a new short-lived process, renews it.
	if _, err := s.Claim("job", owner(t, "cli-lane", "sess-cli-lane")); err != nil {
		t.Fatalf("same-session renew: %v", err)
	}
}
