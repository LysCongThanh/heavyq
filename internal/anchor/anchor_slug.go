package anchor

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxTextSlug = 40 // longest slug taken from item text
	maxFileSlug = 48 // longest slug taken from a file path
)

// slug lowercases s to ASCII letters and digits joined by single dashes, cut
// to max bytes without a trailing dash. It can return "".
func slug(s string, max int) string {
	var b strings.Builder
	dash := true // swallow leading separators
	for _, r := range strings.ToLower(s) {
		if r < unicode.MaxASCII && (unicode.IsLower(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := b.String()
	if len(out) > max {
		out = out[:max]
	}
	return strings.TrimRight(out, "-")
}

// hash6 is a short stable digest used where ASCII slugs lose information.
func hash6(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:6]
}

// textSlug is the slug of item text. Text with non-ASCII letters or digits
// (or no ASCII at all) gets a digest suffix so different texts stay apart.
// The same text always gives the same slug.
func textSlug(text string) string {
	s := slug(text, maxTextSlug)
	for _, r := range text {
		if r >= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if s == "" {
				return hash6(text)
			}
			return s + "-" + hash6(text)
		}
	}
	if s == "" {
		return hash6(text)
	}
	return s
}

// fileSlug identifies a source file inside ids: the repo-relative slash path
// without extension, slugged. Long paths keep a prefix plus a digest.
func fileSlug(rel string) string {
	base := strings.TrimSuffix(rel, path.Ext(rel))
	s := slug(base, 1<<10)
	if len(s) <= maxFileSlug {
		if s == "" {
			return hash6(rel)
		}
		return s
	}
	return strings.TrimRight(s[:maxFileSlug-7], "-") + "-" + hash6(rel)
}

// uniqueID returns id, or id with the first free "-N" suffix (N>=2) when it
// is taken, and records the result. dup says a suffix was needed.
func uniqueID(used map[string]bool, id string) (out string, dup bool) {
	if !used[id] {
		used[id] = true
		return id, false
	}
	for n := 2; ; n++ {
		suffix := "-" + strconv.Itoa(n)
		base := id
		if len(base)+len(suffix) > MaxIDLen {
			base = strings.TrimRight(base[:MaxIDLen-len(suffix)], "-.")
		}
		if cand := base + suffix; !used[cand] {
			used[cand] = true
			return cand, true
		}
	}
}
