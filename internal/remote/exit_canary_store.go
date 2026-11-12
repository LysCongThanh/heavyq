package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// How long a canary verdict is reused. A pass holds for a day; a failure is checked again sooner, so a fixed
// remote is trusted again without anyone deleting a file.
const (
	CanaryPassTTL = 24 * time.Hour
	CanaryFailTTL = time.Hour
)

// CanaryVerdict is the stored answer of the exit canary for one machine.
type CanaryVerdict struct {
	Key     string    `json:"key"` // CanaryKey of the machine when it was checked
	Trusted bool      `json:"trusted"`
	Reason  string    `json:"reason,omitempty"` // why not trusted
	At      time.Time `json:"at"`
}

// Fresh reports whether v may still be used at now.
func (v CanaryVerdict) Fresh(now time.Time) bool {
	ttl := CanaryPassTTL
	if !v.Trusted {
		ttl = CanaryFailTTL
	}
	return now.Sub(v.At) < ttl && !v.At.After(now.Add(time.Minute))
}

// CanaryKey identifies what a verdict was about: this job script, and how the machine is reached and set up
// (transport, work_dir, env_path). Any change asks the question again.
func CanaryKey(m config.MachineConfig) string {
	h := sha256.New()
	for _, part := range []string{jobScript, strings.Join(m.Transport, "\x1f"), m.WorkDir, m.EnvPath} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// canaryFile is where the verdict of machine name is kept under dir.
func canaryFile(dir, name string) string { return filepath.Join(dir, name+".json") }

// LoadCanary returns the stored verdict for m when it is about m as configured now (same CanaryKey). ok is
// false when there is none, it cannot be read, or it is about another script or setup.
func LoadCanary(dir string, m config.MachineConfig) (v CanaryVerdict, ok bool) {
	if dir == "" || !config.SafeName.MatchString(m.Name) {
		return v, false
	}
	b, err := os.ReadFile(canaryFile(dir, m.Name))
	if err != nil || json.Unmarshal(b, &v) != nil || v.Key != CanaryKey(m) {
		return CanaryVerdict{}, false
	}
	return v, true
}

// SaveCanary stores v (its Key set to m's) for m, atomically.
func SaveCanary(dir string, m config.MachineConfig, v CanaryVerdict) error {
	if dir == "" || !config.SafeName.MatchString(m.Name) {
		return nil
	}
	v.Key = CanaryKey(m)
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+m.Name+"-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), canaryFile(dir, m.Name))
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
	}
	return werr
}
