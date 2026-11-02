package pools

import (
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func testSet(t *testing.T, ps ...Pool) *Set {
	t.Helper()
	s := NewSet(t.TempDir(), ps)
	s.Eph = PortRange{First: 32768, Last: 60999}
	return s
}

func plain(name string, vals ...string) Pool { return Pool{Name: name, Values: vals} }

func TestAcquireHandsOutDistinctValuesAndReleases(t *testing.T) {
	s := testSet(t, plain("pg", "a", "b"))
	need := []Need{{"pg", 1}}
	l1, short, err := s.Acquire(need)
	if err != nil || short != nil {
		t.Fatalf("first: %v %v", short, err)
	}
	l2, short, _ := s.Acquire(need)
	if short != nil || l1.Values()["pg"][0] == l2.Values()["pg"][0] {
		t.Fatalf("second lease must get the other value: %v %v", l1.Values(), l2.Values())
	}
	_, short, _ = s.Acquire(need)
	if short == nil || short.Free != 0 || short.Capacity != 2 {
		t.Fatalf("pool is exhausted, got %+v", short)
	}
	if r := short.Reason([]string{"a", "b"}, 2); r != "waiting for pg: 2/2 held by a, b" {
		t.Fatalf("reason = %q", r)
	}
	l1.Release()
	l1.Release() // repeated release is harmless
	l3, short, _ := s.Acquire(need)
	if short != nil || l3.Values()["pg"][0] != "a" {
		t.Fatalf("released value must be handed out again: %v %v", short, l3.Values())
	}
	if env := l3.Env(); !reflect.DeepEqual(env, []string{"HEAVYQ_PG=a"}) {
		t.Fatalf("env = %v", env)
	}
}

// A ticket that needs port:2 and pg while pg is taken must hold no port.
func TestAcquireIsAllOrNothing(t *testing.T) {
	s := testSet(t, plain("port", "1", "2", "3"), plain("pg", "a"))
	pg, _, _ := s.Acquire([]Need{{"pg", 1}})
	_, short, err := s.Acquire([]Need{{"port", 2}, {"pg", 1}})
	if err != nil || short == nil || short.Pool != "pg" {
		t.Fatalf("want a shortfall on pg, got %+v, %v", short, err)
	}
	// All three ports must still be free: nothing was kept.
	all, short, _ := s.Acquire([]Need{{"port", 3}})
	if short != nil {
		t.Fatalf("a failed acquire kept ports: %+v", short)
	}
	all.Release()
	pg.Release()
	both, short, _ := s.Acquire([]Need{{"port", 2}, {"pg", 1}})
	if short != nil || len(both.Values()["port"]) != 2 {
		t.Fatalf("after pg is free the ticket fits: %+v", short)
	}
	if env := both.Env(); !reflect.DeepEqual(env, []string{"HEAVYQ_PORT_1=1", "HEAVYQ_PORT_2=2", "HEAVYQ_PG=a"}) {
		t.Fatalf("env = %v", env)
	}
}

func TestCheckIsDryAndSkipsUnknownPools(t *testing.T) {
	s := testSet(t, plain("pg", "a"))
	if sf := s.Check([]Need{{"pg", 1}, {"elsewhere", 5}}); sf != nil {
		t.Fatalf("free pool: %+v", sf)
	}
	l, _, _ := s.Acquire([]Need{{"pg", 1}})
	if sf := s.Check([]Need{{"pg", 1}}); sf == nil || sf.Free != 0 {
		t.Fatalf("held pool: %+v", sf)
	}
	l.Release()
	if sf := s.Check([]Need{{"pg", 1}}); sf != nil {
		t.Fatalf("Check must not keep a lock: %+v", sf)
	}
}

func TestValidateRefusesWhatCanNeverBeMet(t *testing.T) {
	s := testSet(t, plain("pg", "a"), Pool{Name: "port", Port: true, Values: []string{"4100", "40000"}})
	for _, tc := range []struct {
		needs []Need
		want  string
	}{
		{[]Need{{"nope", 1}}, "unknown pool"},
		{[]Need{{"pg", 2}}, "has 1 values"},
		{[]Need{{"port", 2}}, "ephemeral range"}, // 40000 is inside 32768-60999
	} {
		if err := s.Validate(tc.needs); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want %q", tc.needs, err, tc.want)
		}
	}
	if err := s.Validate([]Need{{"pg", 1}, {"port", 1}}); err != nil {
		t.Fatal(err)
	}
}

func TestPortPoolSkipsEphemeralAndBoundPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener:", err)
	}
	defer func() { _ = ln.Close() }()
	busy := ln.Addr().(*net.TCPAddr).Port
	free := freePort(t, busy)
	s := testSet(t, Pool{Name: "port", Port: true, Values: []string{strconv.Itoa(busy), strconv.Itoa(free), "40000"}})
	s.Eph = PortRange{First: 40000, Last: 40010}
	if got := s.Capacity("port"); got != 2 {
		t.Fatalf("capacity = %d, want 2 (40000 is ephemeral)", got)
	}
	l, short, err := s.Acquire([]Need{{"port", 1}})
	if err != nil || short != nil || l.Values()["port"][0] != strconv.Itoa(free) {
		t.Fatalf("got %v %+v %v; want the unbound port %d", l.Values(), short, err, free)
	}
	_, short, _ = s.Acquire([]Need{{"port", 1}})
	if short == nil || short.Free != 0 {
		t.Fatalf("the bound port and the leased one are both out: %+v", short)
	}
	if r := short.Reason([]string{"x"}, 1); r != "waiting for port: 2/2 held by x (1 in use outside the queue)" {
		t.Fatalf("reason = %q", r)
	}
}

// freePort returns a currently unused loopback port other than not.
func freePort(t *testing.T, not int) int {
	t.Helper()
	for range 20 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if p != not {
			return p
		}
	}
	t.Fatal("no free port")
	return 0
}
