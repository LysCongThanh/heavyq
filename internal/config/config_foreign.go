package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ForeignConfig is one [[foreign]] table: another queue that heavyq only observes (`heavyq fleet`). heavyq
// never writes to it, never takes its slots and never schedules on its behalf; it reads the queue's history,
// its live tickets and its machine files, and probes its slot locks on each machine.
type ForeignConfig struct {
	// Name labels the queue in reports; it must not be "heavyq".
	Name string `toml:"name"`
	// History is the queue's run history on this machine: one line per finished run.
	History string `toml:"history"`
	// Format of History; "tsv" (tab-separated, the default) is the one layout supported.
	Format string `toml:"format"`
	// Columns name the History columns in order (default: ForeignColumns). Unknown names are refused.
	Columns []string `toml:"columns"`
	// Tickets is the directory of live tickets: "<prio>-<20-digit unix ns>-<pid>-<lane>", ".run" while running.
	Tickets string `toml:"tickets"`
	// InfraExits are exit codes of this queue that mean infrastructure, not code (added to 127, 137 and 255).
	InfraExits []int `toml:"infra_exits"`
	// MachinesDir holds one "<machine>.env" file per machine, KEY=VALUE lines (SLOTS, LIGHT_SLOTS, KIND).
	MachinesDir string `toml:"machines_dir"`
	// SlotDirs maps a heavyq machine name to the directory of this queue's slot lock files on that machine
	// (absolute, or relative to that machine's home). A slot is busy while its file is flock-held.
	SlotDirs map[string]string `toml:"slot_dirs"`
	// SlotGlobs maps a heavyq machine name to an absolute (or ~/) glob of this queue's slot lock files on that
	// machine, for slot files that share a directory with others ("/tmp/q-[0-9]*.lock"): one "*" and any
	// "[0-9]" classes, in the base name only. A slot is busy while its file is flock-held.
	SlotGlobs map[string]string `toml:"slot_globs"`
	// Aliases map a machine name the queue writes to the heavyq machine name ("build-1" = "local").
	Aliases map[string]string `toml:"aliases"`
}

// ForeignColumns are the History columns heavyq understands, and the default layout, in this order.
var ForeignColumns = []string{"end_epoch", "machine", "lane", "prio", "light", "wait_s", "run_s", "exit"}

// foreignRequiredColumns: without them a line cannot be placed in the window, on a machine, or judged.
var foreignRequiredColumns = []string{"end_epoch", "machine", "exit"}

// ForeignFormatTSV is the default (and only) history format.
const ForeignFormatTSV = "tsv"

// MachineName maps a machine name written by the foreign queue to the heavyq machine name.
func (f ForeignConfig) MachineName(name string) string {
	if to, ok := f.Aliases[name]; ok {
		return to
	}
	return name
}

// normalizeForeign validates the [[foreign]] tables; it runs after the machines are known.
func (c *Config) normalizeForeign() error {
	machines := c.FleetMachineNames()
	seen := map[string]bool{}
	for i := range c.Foreign {
		f := &c.Foreign[i]
		if !SafeName.MatchString(f.Name) || f.Name == "heavyq" || seen[f.Name] {
			return fmt.Errorf("[[foreign]] #%d: name %q must be unique (listed twice?), not \"heavyq\", and match %s", i+1, f.Name, SafeName)
		}
		seen[f.Name] = true
		if err := f.normalize(machines); err != nil {
			return fmt.Errorf("foreign %q: %w", f.Name, err)
		}
	}
	return nil
}

func (f *ForeignConfig) normalize(machines []string) error {
	if f.History == "" && f.Tickets == "" {
		return errors.New("set history or tickets (or both)")
	}
	if f.Format == "" {
		f.Format = ForeignFormatTSV
	}
	if f.Format != ForeignFormatTSV {
		return fmt.Errorf("format %q: only %q is supported", f.Format, ForeignFormatTSV)
	}
	if len(f.Columns) == 0 {
		f.Columns = slices.Clone(ForeignColumns)
	}
	if err := checkColumns(f.Columns); err != nil {
		return err
	}
	for _, p := range []struct {
		key string
		v   *string
	}{{"history", &f.History}, {"tickets", &f.Tickets}, {"machines_dir", &f.MachinesDir}} {
		if err := localPath(p.key, p.v); err != nil {
			return err
		}
	}
	for _, e := range f.InfraExits {
		if e < 1 || e > 255 {
			return fmt.Errorf("infra_exits: %d is not an exit code 1-255", e)
		}
	}
	for m, dir := range f.SlotDirs {
		if !slices.Contains(machines, m) {
			return fmt.Errorf("slot_dirs: %q is not a configured machine (%s)", m, strings.Join(machines, ", "))
		}
		if !safeRemoteDir(dir) {
			return fmt.Errorf("slot_dirs: %q for %s is empty or has unsafe characters", dir, m)
		}
	}
	if err := f.checkSlotGlobs(machines); err != nil {
		return err
	}
	for from, to := range f.Aliases {
		if !SafeName.MatchString(from) || !slices.Contains(machines, to) {
			return fmt.Errorf("aliases: %q = %q must map a plain name to a configured machine (%s)", from, to, strings.Join(machines, ", "))
		}
	}
	return nil
}

func checkColumns(cols []string) error {
	seen := map[string]bool{}
	for _, c := range cols {
		if !slices.Contains(ForeignColumns, c) {
			return fmt.Errorf("columns: unknown column %q (known: %s)", c, strings.Join(ForeignColumns, ", "))
		}
		if seen[c] {
			return fmt.Errorf("columns: %q listed twice", c)
		}
		seen[c] = true
	}
	for _, c := range foreignRequiredColumns {
		if !seen[c] {
			return fmt.Errorf("columns: the history needs the column %q", c)
		}
	}
	return nil
}

// localPath expands "~/" and requires an absolute, clean path on this machine ("" stays unset).
func localPath(key string, p *string) error {
	v := *p
	if v == "" {
		return nil
	}
	if strings.HasPrefix(v, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		v = filepath.Join(home, v[2:])
	}
	if !filepath.IsAbs(v) || strings.ContainsAny(v, "\x00\n") {
		return fmt.Errorf("%s %q must be an absolute path (or start with ~/)", key, *p)
	}
	*p = filepath.Clean(v)
	return nil
}

// safeRemoteDir accepts a directory that can be passed to a remote shell as one quoted word and cannot
// climb out of where it says: no quotes, $, backticks, backslashes, spaces, newlines or "..".
func safeRemoteDir(d string) bool {
	return d != "" && !strings.ContainsAny(d, "'\"`$\\\n\x00 ;&|<>*?") && !strings.Contains(d, "..")
}
