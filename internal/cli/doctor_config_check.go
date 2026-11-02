package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// printConfigCheck is the config check of `heavyq doctor`. It loads the user
// config itself, so it runs even when the command table would have refused it:
// a config that does not load is printed with its error (and reports false), a
// config with keys this heavyq does not know lists them (they are ignored: a
// newer heavyq may define them), and a clean one says so. Nothing is printed
// when there is no config file.
func printConfigCheck(w io.Writer, path string, explicit bool) (ok bool) {
	cfg, err := config.Load(path, explicit)
	if err != nil {
		fmt.Fprintf(w, "config: does not load: %v\n", err)
		return false
	}
	switch {
	case cfg.Path == "":
	case len(cfg.Unknown) == 0:
		fmt.Fprintf(w, "config: %s: ok\n", cfg.Path)
	default:
		fmt.Fprintf(w, "config: %s: %d key(s) this heavyq does not know, ignored (a newer heavyq may define them):\n", cfg.Path, len(cfg.Unknown))
		for _, u := range cfg.Unknown {
			if u.Line > 0 {
				fmt.Fprintf(w, "  line %d: %s\n", u.Line, u.Key)
			} else {
				fmt.Fprintf(w, "  %s\n", u.Key)
			}
		}
	}
	return true
}
