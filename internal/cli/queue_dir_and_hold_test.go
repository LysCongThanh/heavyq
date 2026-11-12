package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// A run started without XDG_RUNTIME_DIR (cron, `env -i`, a service) must find
// the same queue and slot locks as one started from a login session: the
// per-user runtime directory exists either way, so it is used either way.
func TestQueueDirIsTheSameWithAndWithoutXDGRuntimeDir(t *testing.T) {
	runUser := filepath.Join(t.TempDir(), strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(runUser, 0o700); err != nil {
		t.Fatal(err)
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	login := queueDirFrom(env(map[string]string{"XDG_RUNTIME_DIR": runUser}), runUser, "")
	cron := queueDirFrom(env(map[string]string{}), runUser, "")
	if login != cron || login != filepath.Join(runUser, "heavyq") {
		t.Fatalf("login session %q, cron %q", login, cron)
	}
	// No runtime directory at all: the per-user temp directory, not a guess.
	if got := queueDirFrom(env(map[string]string{}), filepath.Join(runUser, "missing"), "/var/tmp-of-user"); got !=
		filepath.Join("/var/tmp-of-user", "heavyq-"+strconv.Itoa(os.Getuid())) {
		t.Fatalf("fallback %q", got)
	}
	// A runtime directory of another user (or open to others) is not used.
	if err := os.Chmod(runUser, 0o777); err != nil {
		t.Fatal(err)
	}
	if got := queueDirFrom(env(map[string]string{}), runUser, "/t"); got == filepath.Join(runUser, "heavyq") {
		t.Fatalf("a runtime directory others can write is not ours: %q", got)
	}
	// HEAVYQ_DIR always wins.
	if got := queueDirFrom(env(map[string]string{"HEAVYQ_DIR": "/q", "XDG_RUNTIME_DIR": runUser}), runUser, ""); got != "/q" {
		t.Fatalf("HEAVYQ_DIR: %q", got)
	}
}

// The RAM held for just-started jobs counts only during hold_for after the
// claim. After the clock stepped back, a claim seems to lie in the future: that
// is not "just started", so nothing stays held for an hour.
func TestRAMHoldIgnoresClaimsFromTheFuture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[[remote]]\nname = \"box\"\ntransport = [\"sh\", \"-c\"]\nwork_dir = \"/w\"\ngit_url = \"/w/git\"\njob_mb = 512\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(at time.Time) queue.Snapshot {
		return queue.Snapshot{Running: []*queue.Ticket{{Machine: "box", Slot: "h1", ClaimedAt: at}}}
	}
	probes := newProbeCache(time.Second)
	if v := buildView(cfg, "box", snap(time.Now()), probes, time.Minute); v.state.ReservedMB != 512 {
		t.Fatalf("a job claimed just now holds its RAM: %+v", v.state)
	}
	if v := buildView(cfg, "box", snap(time.Now().Add(time.Hour)), probes, time.Minute); v.state.ReservedMB != 0 {
		t.Fatalf("a claim an hour in the future (clock stepped back) holds RAM: %+v", v.state)
	}
}
