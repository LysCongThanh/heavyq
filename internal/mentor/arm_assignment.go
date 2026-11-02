package mentor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultHoldoutPct is the share of eligible events held out: 20, so 80% of
// them are shown.
const DefaultHoldoutPct = 20

// Assign draws the arm of one event. It is a pure function of the seed, the
// rule and the event (the ledger record that raised it), so a given seed
// always gives the same arms and a reader of the log can verify every draw:
// the first 8 bytes of sha256(seed 0 rule 0 event) modulo 100 are below
// holdoutPct for the holdout arm. holdoutPct 0 injects always, 100 never
// (shadow mode: events are logged and measured but the agent sees nothing).
func Assign(seed, rule, event string, holdoutPct int) Arm {
	h := sha256.Sum256([]byte(seed + "\x00" + rule + "\x00" + event))
	if int(binary.BigEndian.Uint64(h[:8])%100) < holdoutPct {
		return ArmHoldout
	}
	return ArmInject
}

// seedRe bounds an explicit seed to a plain, loggable tag.
var seedRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// seedEnv overrides the stored seed (a reproducible experiment, or a test).
const seedEnv = "HEAVYQ_MENTOR_SEED"

// Seed returns the experiment seed: $HEAVYQ_MENTOR_SEED when it is a plain tag,
// else the one stored in <dir>/seed, created with 64 random bits on first use
// (O_EXCL, so concurrent first calls agree on one value).
func (s *Store) Seed() (string, error) {
	if v := os.Getenv(seedEnv); v != "" {
		if !seedRe.MatchString(v) {
			return "", fmt.Errorf("%s must be 1-64 characters from A-Za-z0-9._:-", seedEnv)
		}
		return v, nil
	}
	path := filepath.Join(s.Dir, "seed")
	if v, ok := readSeed(path); ok {
		return v, nil
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	switch {
	case err == nil:
		_, werr := f.WriteString(hex.EncodeToString(b[:]) + "\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return "", werr
		}
	case !errors.Is(err, fs.ErrExist):
		return "", err
	}
	if v, ok := readSeed(path); ok {
		return v, nil
	}
	return "", errors.New("mentor seed file is unreadable: " + path)
}

func readSeed(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(b))
	return v, seedRe.MatchString(v)
}
