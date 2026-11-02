package history_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/history/historytest"
)

// sampleLine is a realistic ledger line (what the writer produces).
func sampleLine(tb testing.TB) []byte {
	tb.Helper()
	r := historytest.Record(7, historytest.Options{N: 100, End: time.Now()})
	b, err := json.Marshal(r)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func BenchmarkDecodeRecordFast(b *testing.B) {
	line := sampleLine(b)
	b.SetBytes(int64(len(line)))
	for i := 0; i < b.N; i++ {
		if _, err := history.DecodeRecord(line); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeRecordEncodingJSON(b *testing.B) {
	line := sampleLine(b)
	b.SetBytes(int64(len(line)))
	for i := 0; i < b.N; i++ {
		var r history.Record
		if err := json.Unmarshal(line, &r); err != nil {
			b.Fatal(err)
		}
	}
}
