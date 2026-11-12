package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Origin is where a config came from: the file Load was asked to read and a
// digest of what it read there. Two configs with the same Sum were read from
// the same bytes, so they make the same scheduling decisions.
type Origin struct {
	// Path is the file, absolute (a later chdir does not move it); "" when
	// there is none to read (no home directory).
	Path string
	// Required: a missing file is an error, not the defaults.
	Required bool
	// Sum identifies the content: a digest of the file, or noFileSum when the
	// defaults were used. Never "" for a config Load returned.
	Sum string
}

// noFileSum is the Sum of the defaults, used when there is no file.
const noFileSum = "defaults"

// contentSum is the Sum of a file's bytes.
func contentSum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

func absPath(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// Watch notices edits of the file a config was loaded from, for a process
// that outlives one (a ticket waiting for its turn). While the file is
// untouched a check is one stat (for racyWindow after an edit it also re-reads
// the small file); an edit is loaded and validated exactly as Load does at
// start, so the result is what a fresh heavyq would read. An edit is used only
// once two checks in a row read it the same: an editor that writes the file in
// place (truncate, then write) can be read half-way, and half a config (the
// load limit without the slot list after it) is a valid config nobody wrote.
type Watch struct {
	origin Origin
	seen   fileMark // the file as the last check saw it (zero: not looked at yet)
	failed string   // the last refusal returned, so a bad edit is reported once
	// read is what the last check that read the file found there that differs
	// from the caller's config: the Sum of the config, or the refusal.
	read string
}

// fileMark is what a stat says about the file; any change means a re-read.
type fileMark struct {
	exists  bool
	size    int64
	modNano int64
}

// racyWindow: a file modified this recently may change again without its
// size or (coarse) mtime moving, so it is not trusted to be unchanged yet.
const racyWindow = 2 * time.Second

func markOf(path string) fileMark {
	st, err := os.Stat(path)
	if err != nil {
		return fileMark{}
	}
	return fileMark{exists: true, size: st.Size(), modNano: st.ModTime().UnixNano()}
}

// NewWatch watches the file cfg was loaded from. The first check reads it
// once more, so an edit made between Load and NewWatch is not missed.
func NewWatch(cfg *Config) *Watch {
	return &Watch{origin: cfg.Origin, seen: fileMark{size: -1}}
}

// Changed returns the config the file holds now when its content differs from
// cur's, else nil. The first check that reads an edit only remembers it and
// reads the file again on the next check, which returns it if it reads the
// same (else that read is remembered in turn). An edit that does not load
// (syntax, an unsafe or invalid value, an explicit file gone) returns its error
// the first time only; the caller keeps cur, its last good config, and later
// checks stay quiet until the file changes again. A file cur was read from that
// is gone is such an edit, whether it was required or not: a moment without the
// file (an editor replacing it) must not drop a waiting ticket to the defaults.
func (w *Watch) Changed(cur *Config) (*Config, error) {
	if w == nil || w.origin.Path == "" {
		return nil, nil
	}
	m := markOf(w.origin.Path)
	if m == w.seen {
		return nil, nil
	}
	w.seen = m
	if m.exists && time.Since(time.Unix(0, m.modNano)) < racyWindow {
		w.seen = fileMark{size: -1} // a same-size edit in the same clock tick would look untouched: read again next time
	}
	fresh, err := w.load(m, cur)
	found := ""
	switch {
	case err != nil:
		found = "refused: " + err.Error()
	case fresh.Origin.Sum == cur.Origin.Sum:
		w.read, w.failed = "", ""
		return nil, nil
	default:
		found = fresh.Origin.Sum
	}
	if found != w.read { // first sight: the writer may not be done; look again next time
		w.read, w.seen = found, fileMark{size: -1}
		return nil, nil
	}
	if err != nil {
		if err.Error() == w.failed {
			return nil, nil
		}
		w.failed = err.Error()
		return nil, err
	}
	w.failed = ""
	return fresh, nil
}

// Retry makes the next check read the file again even if it is untouched: the
// caller could not use the config Changed returned for a reason outside the file
// (a repository heavyq.toml that does not load), so that config is returned
// again (it reads the same) until the caller can take it or the file changes.
func (w *Watch) Retry() {
	if w != nil {
		w.seen = fileMark{size: -1}
	}
}

// load reads the file as a fresh heavyq would, except that a file cur was read
// from (cur is not the defaults) and is gone now is an error, not the defaults.
func (w *Watch) load(m fileMark, cur *Config) (*Config, error) {
	if !m.exists && cur.Origin.Sum != noFileSum {
		return nil, fmt.Errorf("config file %s is gone", w.origin.Path)
	}
	return Load(w.origin.Path, w.origin.Required)
}
