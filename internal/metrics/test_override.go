package metrics

import (
	"os"
	"strconv"
	"strings"
)

// Test-only metrics override. The e2e tests run the real binary on whatever
// machine the suite happens to run on, so a failure's "under load" class would
// depend on that machine. They pin the reading instead:
//
//	HEAVYQ_TEST=1 HEAVYQ_TEST_FAKE_METRICS="load1=0.1,free_mb=99999,cores=8"
//
// The override is honored only when HEAVYQ_TEST is exactly "1", and only when
// every key parses: a malformed spec is ignored (the real reading is used),
// never half-applied. Keys: load1 (float, >= 0), free_mb, total_mb, cores
// (cores must be >= 1). An unlisted key is 0, except cores, which keeps the
// host's. It is documented in CONTRIBUTING.md.
const (
	envTestMode    = "HEAVYQ_TEST"
	envFakeMetrics = "HEAVYQ_TEST_FAKE_METRICS"
)

// fakeFromEnv returns the pinned reading and true when the override is on.
func fakeFromEnv(real Metrics) (Metrics, bool) {
	if os.Getenv(envTestMode) != "1" {
		return Metrics{}, false
	}
	return parseFake(os.Getenv(envFakeMetrics), real.Cores)
}

// parseFake parses "load1=0.1,free_mb=99999,cores=8". defaultCores is used when
// the spec has no cores key.
func parseFake(spec string, defaultCores int) (Metrics, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Metrics{}, false
	}
	m := Metrics{Cores: defaultCores, Known: true}
	for _, kv := range strings.Split(spec, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			return Metrics{}, false
		}
		var err error
		switch key {
		case "load1":
			m.Load1, err = strconv.ParseFloat(val, 64)
			if err == nil && m.Load1 < 0 {
				return Metrics{}, false
			}
		case "free_mb":
			m.FreeMB, err = strconv.Atoi(val)
		case "total_mb":
			m.TotalMB, err = strconv.Atoi(val)
		case "cores":
			m.Cores, err = strconv.Atoi(val)
		default:
			return Metrics{}, false
		}
		if err != nil {
			return Metrics{}, false
		}
	}
	if m.Cores < 1 || m.FreeMB < 0 || m.TotalMB < 0 {
		return Metrics{}, false
	}
	return m, true
}
