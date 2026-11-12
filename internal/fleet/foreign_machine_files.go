package fleet

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MachineSlots is what a foreign queue's machine file says about one machine.
type MachineSlots struct {
	Heavy int    // words in SLOTS
	Light int    // words in LIGHT_SLOTS
	Kind  string // KIND, e.g. linux-x64
}

// machineFileName is "<machine>.env"; backups and other files are not machines.
var machineFileName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]{0,63})\.env$`)

// ReadMachineFiles reads a directory of KEY=VALUE machine files, by machine name. The files are parsed,
// never sourced: only SLOTS, LIGHT_SLOTS and KIND are read, quotes stripped, everything else ignored.
func ReadMachineFiles(dir string) (map[string]MachineSlots, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]MachineSlots{}
	for _, e := range ents {
		m := machineFileName.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		if ms, ok := readMachineFile(filepath.Join(dir, e.Name())); ok {
			out[m[1]] = ms
		}
	}
	return out, nil
}

func readMachineFile(path string) (MachineSlots, bool) {
	f, err := os.Open(path)
	if err != nil {
		return MachineSlots{}, false
	}
	defer f.Close()
	var ms MachineSlots
	sc := bufio.NewScanner(f)
	for n := 0; sc.Scan() && n < 1000; n++ {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimPrefix(k, "export ") {
		case "SLOTS":
			ms.Heavy = len(strings.Fields(v))
		case "LIGHT_SLOTS":
			ms.Light = len(strings.Fields(v))
		case "KIND":
			ms.Kind = v
		}
	}
	return ms, true
}
