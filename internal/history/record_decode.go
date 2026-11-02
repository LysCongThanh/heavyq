package history

import "encoding/json"

// decodeRecord parses one ledger line. Lines heavyq writes are a flat object of
// plain scalars, which fastDecode reads several times faster than reflection
// (the decode is most of the cost of a tail read: 2,000 records). Anything
// else, a line that is odd in any way, goes through encoding/json, so the
// result and the errors are those of json.Unmarshal in every case.
func decodeRecord(line []byte) (Record, error) {
	if r, ok := fastDecode(line); ok {
		return r, nil
	}
	var r Record
	err := json.Unmarshal(line, &r)
	return r, err
}

// fastDecode reads a flat JSON object whose keys are exactly the Record
// fields, with strings (escapes included, surrogate pairs not), strict-grammar
// numbers and booleans. ok is false for anything else (unknown or
// differently-cased key, nested value, escape, wrong type, trailing data): the
// caller then asks encoding/json, which is always right.
func fastDecode(b []byte) (r Record, ok bool) {
	ln := lineBuf{b: b, s: string(b)} // one copy: the string fields of r are slices of it
	i := skipSpace(b, 0)
	if i >= len(b) || b[i] != '{' {
		return r, false
	}
	i = skipSpace(b, i+1)
	if i < len(b) && b[i] == '}' {
		return r, skipSpace(b, i+1) == len(b)
	}
	for {
		key, ni, kok := plainString(b, i)
		if !kok {
			return r, false
		}
		i = skipSpace(b, ni)
		if i >= len(b) || b[i] != ':' {
			return r, false
		}
		i = skipSpace(b, i+1)
		if i, ok = r.set(string(key), ln, i); !ok {
			return r, false
		}
		i = skipSpace(b, i)
		if i >= len(b) {
			return r, false
		}
		switch b[i] {
		case ',':
			i = skipSpace(b, i+1)
		case '}':
			return r, skipSpace(b, i+1) == len(b)
		default:
			return r, false
		}
	}
}

// set parses the value at b[i:] into the field named key and returns the index
// after it. ok is false for an unknown key or a value of the wrong kind.
func (r *Record) set(key string, ln lineBuf, i int) (int, bool) {
	var ok bool
	switch key {
	case "v":
		r.V, i, ok = intValue(ln.b, i)
	case "id":
		r.ID, i, ok = ln.str(i)
	case "end":
		i, ok = timeValue(&r.End, ln.b, i)
	case "machine":
		r.Machine, i, ok = ln.str(i)
	case "lane":
		r.Lane, i, ok = ln.str(i)
	case "prio":
		r.Prio, i, ok = intValue(ln.b, i)
	case "light":
		r.Light, i, ok = boolValue(ln.b, i)
	case "wait_s":
		r.WaitSecs, i, ok = floatValue(ln.b, i)
	case "run_s":
		r.RunSecs, i, ok = floatValue(ln.b, i)
	case "exit":
		r.Exit, i, ok = intValue(ln.b, i)
	case "cmd":
		r.Cmd, i, ok = ln.str(i)
	case "cmd_hash":
		r.CmdHash, i, ok = ln.str(i)
	case "run_id":
		r.RunID, i, ok = ln.str(i)
	case "cache":
		r.Cache, i, ok = ln.str(i)
	case "cache_note":
		r.CacheNote, i, ok = ln.str(i)
	case "result":
		r.Result, i, ok = ln.str(i)
	case "forced":
		r.Forced, i, ok = boolValue(ln.b, i)
	case "load1":
		r.Load1, i, ok = floatValue(ln.b, i)
	case "free_mb":
		r.FreeMB, i, ok = intValue(ln.b, i)
	case "cores":
		r.Cores, i, ok = intValue(ln.b, i)
	case "max_load":
		r.MaxLoad, i, ok = floatValue(ln.b, i)
	case "job_mb":
		r.JobMB, i, ok = intValue(ln.b, i)
	case "cpu_s":
		r.CPUSecs, i, ok = floatValue(ln.b, i)
	case "max_rss_mb":
		r.MaxRSSMB, i, ok = intValue(ln.b, i)
	case "needs":
		r.Needs, i, ok = ln.str(i)
	case "pools":
		r.Pools, i, ok = ln.str(i)
	case "session":
		r.Session, i, ok = ln.str(i)
	case "item":
		r.Item, i, ok = ln.str(i)
	case "gate":
		r.Gate, i, ok = ln.str(i)
	case "repo":
		r.Repo, i, ok = ln.str(i)
	case "worktree":
		r.Worktree, i, ok = ln.str(i)
	case "subdir":
		r.Subdir, i, ok = ln.str(i)
	case "commit":
		r.Commit, i, ok = ln.str(i)
	case "tree":
		r.Tree, i, ok = ln.str(i)
	case "dirty":
		r.Dirty, i, ok = boolValue(ln.b, i)
	}
	return i, ok // an unknown key leaves ok false
}
