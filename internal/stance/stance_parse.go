package stance

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// Frontmatter format: TOML between two lines that read exactly "+++", then a
// markdown body.
//
//	+++
//	name = "breakthrough"
//	+++
//	The body...
//
// Why TOML and not YAML: heavyq already depends on BurntSushi/toml (no new
// dependency), TOML is strictly typed (a number is never a string by
// accident), and its decoder reports every key it did not recognise, so a
// typo like "cooldown_minute" is an error instead of a silently ignored
// setting. A YAML-ish hand parser would have to rebuild all of that.
const fence = "+++"

// Parse decodes one stance file. path is used only in error messages and to
// derive the expected name: the file name without ".md" must equal the
// stance's name (or the name may be left out). Errors read "path:line: ...".
// The Source of the result is left for the caller to set.
func Parse(path string, data []byte) (Stance, error) {
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	if !utf8.ValidString(text) {
		return Stance{}, fmt.Errorf("%s: not valid UTF-8", path)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n") // keeps line numbers
	front, body, err := splitFrontmatter(text)
	if err != nil {
		return Stance{}, fmt.Errorf("%s:%w", path, err)
	}
	var s Stance
	md, err := toml.Decode(front, &s)
	if err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) && pe.Position.Line > 0 {
			return Stance{}, fmt.Errorf("%s:%d: %s", path, pe.Position.Line+1, pe.Message)
		}
		return Stance{}, fmt.Errorf("%s: %w", path, err)
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		if line := unknownKeyLine(front, undec[0]); line > 0 {
			return Stance{}, fmt.Errorf("%s:%d: unknown key %q", path, line+1, undec[0].String())
		}
		return Stance{}, fmt.Errorf("%s: unknown key %q", path, undec[0].String())
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".md")
	if err := s.finish(stem, md.IsDefined("inject_tokens"), body); err != nil {
		return Stance{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// splitFrontmatter returns the TOML between the fences and the body after
// them. Errors start with the line number ("1: ..."), so callers can prefix
// the path with ":".
func splitFrontmatter(text string) (front, body string, err error) {
	lines := strings.SplitAfter(text, "\n")
	if strings.TrimRight(lines[0], " \t\n") != fence {
		return "", "", errors.New("1: file must start with a +++ line (TOML frontmatter)")
	}
	size := 0
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t\n") == fence {
			return strings.Join(lines[1:i], ""), strings.Join(lines[i+1:], ""), nil
		}
		if size += len(lines[i]); size > maxFrontmatterBytes {
			return "", "", fmt.Errorf("%d: frontmatter larger than %d bytes or the closing +++ is missing", i+1, maxFrontmatterBytes)
		}
	}
	return "", "", fmt.Errorf("%d: frontmatter is not closed by a +++ line", len(lines))
}
