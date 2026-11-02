package steer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxTranscriptScan bounds how much of a transcript the canary check reads.
const maxTranscriptScan = 256 << 20

// NewCanary returns a short random token for one brief, like "hq-3f9a1c2e".
func NewCanary() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "hq-" + hex.EncodeToString(b[:]), nil
}

// CanaryVerdict is what looking for a canary in a transcript found.
type CanaryVerdict int

// The verdicts. Only Missing is a finding; the others say why nothing is.
const (
	CanaryDelivered  CanaryVerdict = iota // the token is in the transcript
	CanaryMissing                         // the transcript was read and has no token
	CanaryUnreadable                      // no usable transcript path, or it could not be read
)

// CheckCanary looks for token in the transcript at path. The path comes from a
// hook input, so it is only read when it is an absolute path to a regular
// .jsonl file; anything else is "unreadable" (best effort, never an error).
func CheckCanary(path, token string) (CanaryVerdict, error) {
	if token == "" || path == "" || !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return CanaryUnreadable, errors.New("no usable transcript path")
	}
	f, err := os.Open(path)
	if err != nil {
		return CanaryUnreadable, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return CanaryUnreadable, errors.New("transcript is not a regular file")
	}
	found, err := scanFor(io.LimitReader(f, maxTranscriptScan), []byte(token))
	switch {
	case err != nil:
		return CanaryUnreadable, err
	case found:
		return CanaryDelivered, nil
	}
	return CanaryMissing, nil
}

// scanFor reports whether needle occurs in r, reading in chunks with overlap so
// a token cut by a chunk boundary is still found.
func scanFor(r io.Reader, needle []byte) (bool, error) {
	const chunk = 1 << 20
	buf := make([]byte, 0, chunk+len(needle))
	tmp := make([]byte, chunk)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if bytes.Contains(buf, needle) {
			return true, nil
		}
		if keep := len(needle) - 1; len(buf) > keep {
			buf = append(buf[:0], buf[len(buf)-keep:]...)
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// CanaryText renders a verdict for `heavyq doctor`.
func CanaryText(v CanaryVerdict, token string, err error) string {
	switch v {
	case CanaryDelivered:
		return fmt.Sprintf("brief delivery: ok (%s reached the transcript)", token)
	case CanaryMissing:
		return fmt.Sprintf("brief delivery: %s is not in the transcript: the SessionStart context may not arrive "+
			"(the transcript can lag a few seconds; otherwise this Claude Code version drops additionalContext)", token)
	}
	msg := "brief delivery: not checked"
	if err != nil {
		msg += " (" + strings.TrimSpace(err.Error()) + ")"
	}
	return msg
}
