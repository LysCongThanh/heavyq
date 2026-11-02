package history

import (
	"encoding/json"
	"strings"
	"testing"
)

// A record with pool needs and values keeps the line flat, so the fast decoder
// reads it, and it agrees with encoding/json.
func TestRecordWithPoolsTakesTheFastPath(t *testing.T) {
	want := Record{V: SchemaVersion, Lane: "a", Cmd: "npm run e2e",
		Needs: "port:2,pg", Pools: "port=4100,4101 pg=app_wt1"}
	line, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fastDecode(line)
	if !ok || got != want {
		t.Fatalf("fast path: ok=%v\n got %+v\nwant %+v", ok, got, want)
	}
	sameAsJSON(t, string(line))
	plain, _ := json.Marshal(Record{Cmd: "x"})
	if s := string(plain); strings.Contains(s, `"needs"`) || strings.Contains(s, `"pools"`) {
		t.Fatalf("a run without needs must not carry the fields: %s", s)
	}
}
