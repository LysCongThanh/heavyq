package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndReadLast(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "history.jsonl")
	for i := 0; i < 5; i++ {
		if err := Append(p, Record{End: time.Now(), Machine: "local", Lane: "api", Prio: i + 1, Exit: i}); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("not json\n")
	f.Close()
	recs, err := ReadLast(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0].Prio != 3 || recs[2].Exit != 4 {
		t.Fatalf("got %+v", recs)
	}
}

func TestReadMissing(t *testing.T) {
	recs, err := ReadLast(filepath.Join(t.TempDir(), "none"), 10)
	if err != nil || recs != nil {
		t.Fatalf("missing file: %v %v", recs, err)
	}
}
