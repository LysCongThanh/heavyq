package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/fleet"
)

const fleetUsage = `usage: heavyq [--config FILE] fleet [--since 24h] [--timeout 10s] [--json] [--md FILE] [--agent]

Observe every machine and every queue, read-only: per machine load/cores, free RAM, memory pressure, an
OVERLOAD flag, the busiest processes by queue and lane, busy slots by owner and a suggested worker cap; per
queue (heavyq and each [[foreign]] queue) runs, run-hours, waits p50/p90/p95, red runs split infra vs code,
and the live running and waiting tickets. --md writes a markdown report atomically (for cron and other
agents). See docs/fleet.md.
`

const (
	defaultFleetWindow  = 24 * time.Hour
	defaultFleetTimeout = 10 * time.Second
	maxFleetWindow      = 366 * 24 * time.Hour
)

// fleetCommand implements `heavyq fleet`.
func fleetCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fleet", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, fleetUsage) }
	since := fs.Duration("since", defaultFleetWindow, "history window, e.g. 6h or 168h")
	timeout := fs.Duration("timeout", defaultFleetTimeout, "per-machine probe timeout")
	asJSON := fs.Bool("json", false, "machine-readable output (schema/fleet.schema.json)")
	mdPath := fs.String("md", "", "also write a markdown report to FILE, atomically")
	agent := fs.Bool("agent", false, "compact digest under 4 KB (automatic in agent mode)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage
	}
	if *since <= 0 || *since > maxFleetWindow {
		fmt.Fprintf(stderr, "heavyq: fleet --since %s: must be above 0 and at most %s\n", *since, maxFleetWindow)
		return ExitUsage
	}
	if *timeout < 100*time.Millisecond || *timeout > 5*time.Minute {
		fmt.Fprintf(stderr, "heavyq: fleet --timeout %s: must be between 100ms and 5m\n", *timeout)
		return ExitUsage
	}

	r := collectFleet(cfg, time.Now(), *since, *timeout)
	if *mdPath != "" {
		if err := fleet.WriteFileAtomic(*mdPath, fleet.RenderMarkdown(r)); err != nil {
			fmt.Fprintln(stderr, "heavyq: fleet --md:", err)
			return 1
		}
	}
	mode, _ := agentmode.Resolve(os.LookupEnv)
	switch {
	case *asJSON:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(r); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
	case *agent || (*mdPath == "" && mode.Terse()):
		fmt.Fprint(stdout, fleet.RenderAgent(r))
	case *mdPath != "":
		// the report is the file; stdout stays quiet for cron
	default:
		fleet.RenderText(stdout, r)
	}
	return 0
}
