package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/cache"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
)

// cacheCommand implements `heavyq cache ls|stats|clear`.
func cacheCommand(cfg *config.Config, args []string, w, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: heavyq cache ls [--json] | stats [--json] | clear")
		return ExitUsage
	}
	fs := flag.NewFlagSet("cache "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	c := cfg.Cache
	st := &cache.Store{Dir: cache.DefaultDir(), TTL: c.TTL.Duration,
		MaxBytes: int64(c.MaxSizeMB) << 20, MaxEntryBytes: int64(c.MaxEntryMB) << 20}
	if st.Dir == "" {
		fmt.Fprintln(stderr, "heavyq: no cache directory (set XDG_CACHE_HOME or HOME)")
		return ExitUsage
	}
	switch args[0] {
	case "ls":
		return cacheList(st, *asJSON, w, stderr)
	case "stats":
		return cacheStats(st, cfg, *asJSON, w, stderr)
	case "clear":
		n, err := st.Clear()
		if err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		fmt.Fprintf(w, "removed %d cached results from %s\n", n, st.Dir)
		return 0
	}
	fmt.Fprintf(stderr, "heavyq: unknown cache command %q (ls, stats, clear)\n", args[0])
	return ExitUsage
}

func cacheList(st *cache.Store, asJSON bool, w, stderr io.Writer) int {
	metas, err := st.List()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	if asJSON {
		if metas == nil {
			metas = []cache.Meta{}
		}
		return writeJSON(w, stderr, metas)
	}
	if len(metas) == 0 {
		fmt.Fprintf(w, "no cached results (%s)\n", st.Dir)
		return 0
	}
	now := time.Now()
	fmt.Fprintf(w, "%-12s  %-19s %-10s %-12s %4s %9s  %s\n", "KEY", "CREATED", "MACHINE", "LANE", "EXIT", "SIZE", "COMMAND")
	for _, m := range metas {
		created := m.Created.Local().Format("2006-01-02 15:04:05")
		if st.Expired(m, now) {
			created += "*"
		}
		fmt.Fprintf(w, "%-12.12s  %-19s %-10s %-12s %4d %9s  %s\n", m.Key, created, m.Machine, m.Lane, m.Exit, humanBytes(m.Size), m.Cmd)
	}
	return 0
}

type cacheStatsView struct {
	Dir       string `json:"dir"`
	Entries   int    `json:"entries"`
	Bytes     int64  `json:"bytes"`
	MaxBytes  int64  `json:"max_bytes"`
	TTL       string `json:"ttl"`
	Failures  bool   `json:"cache_failures"`
	Hits      int    `json:"hits"`
	Coalesced int    `json:"coalesced"`
	Misses    int    `json:"misses"`
	Skipped   int    `json:"skipped"`
}

func cacheStats(st *cache.Store, cfg *config.Config, asJSON bool, w, stderr io.Writer) int {
	metas, err := st.List()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	v := cacheStatsView{Dir: st.Dir, Entries: len(metas), MaxBytes: st.MaxBytes, TTL: st.TTL.String(), Failures: cfg.Cache.CacheFailures}
	for _, m := range metas {
		v.Bytes += m.Size
	}
	recs, err := history.ReadLast(history.DefaultPath(), 0)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: warning:", err)
	}
	for _, r := range recs {
		switch r.Cache {
		case "hit":
			v.Hits++
		case "coalesced":
			v.Coalesced++
		case "miss":
			v.Misses++
		case "skip":
			v.Skipped++
		}
	}
	if asJSON {
		return writeJSON(w, stderr, v)
	}
	fmt.Fprintf(w, "dir:        %s\nentries:    %d (%s of %s, ttl %s, cache_failures %t)\n",
		v.Dir, v.Entries, humanBytes(v.Bytes), humanBytes(v.MaxBytes), v.TTL, v.Failures)
	fmt.Fprintf(w, "history:    %d hits, %d coalesced, %d misses (ran), %d not cacheable\n", v.Hits, v.Coalesced, v.Misses, v.Skipped)
	return 0
}

func writeJSON(w, stderr io.Writer, v any) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return 0
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
