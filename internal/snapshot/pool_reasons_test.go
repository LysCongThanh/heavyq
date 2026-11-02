package snapshot

import (
	"strings"
	"testing"
	"time"
)

const poolWhy = "waiting for pg: 3/3 held by a, b, c"

func poolHead(wait float64) Waiting {
	return Waiting{Ticket: Ticket{ID: "t-head", Lane: "e2e", Prio: 3, WaitS: wait, Cmd: "npm run e2e"}, Position: 1,
		BypassInS: -1, Reasons: map[string]string{"local": poolWhy}}
}

// The story names the pool and who holds it, in the words of the wait reason.
func TestStoryNamesThePoolAHeadWaitsFor(t *testing.T) {
	s := Snapshot{Config: Config{ResourceGate: true, ForceAfterS: 900}, GeneratedAt: storyAt, Waiting: []Waiting{poolHead(5)}}
	got := Story(s, nil, storyAt)
	if want := "1 agent waits because pool pg: 3/3 held by a, b, c."; got[0].Text != want {
		t.Fatalf("story = %q, want %q", got[0].Text, want)
	}
	for _, sent := range got { // the bypass skips RAM and load, never a pool
		if strings.Contains(sent.Text, "skips the RAM") {
			t.Fatalf("the story promises a bypass for a pool wait: %q", sent.Text)
		}
	}
}

// A head that waits long for a pool is starved, and the card says what holds it.
func TestStarvedEvidenceMentionsThePool(t *testing.T) {
	s := Snapshot{Config: Config{ResourceGate: true, ForceAfterS: 900}, Waiting: []Waiting{poolHead(700)}}
	es := ComputeExceptions(s, nil, storyAt.Add(time.Hour))
	if len(es) != 1 || es[0].Kind != KindStarved {
		t.Fatalf("exceptions = %+v", es)
	}
	e := es[0]
	if e.Evidence.Detail != poolWhy || !strings.Contains(e.Text, poolWhy) || strings.Contains(e.Text, "bypass applies") {
		t.Fatalf("starved card = %+v", e)
	}
}
