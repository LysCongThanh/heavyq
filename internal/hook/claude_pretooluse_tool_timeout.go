package hook

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Claude Code's Bash tool timeouts when its environment overrides nothing.
const (
	defaultToolTimeoutMS = 120000
	defaultToolMaxMS     = 600000
)

// ToolTimeouts are BASH_DEFAULT_TIMEOUT_MS and BASH_MAX_TIMEOUT_MS as the hook
// process inherits them from Claude Code ("" = unset).
type ToolTimeouts struct{ Default, Max string }

// limits returns Claude Code's effective default and cap: the default is
// BASH_DEFAULT_TIMEOUT_MS or 120000, the cap BASH_MAX_TIMEOUT_MS or 600000
// but never below the default (checked against Claude Code 2.1.322).
func (e ToolTimeouts) limits() (def, capMS int64) {
	def, capMS = defaultToolTimeoutMS, defaultToolMaxMS
	if n, ok := jsParseInt(e.Default); ok && n > 0 {
		def = n
	}
	if n, ok := jsParseInt(e.Max); ok && n > 0 {
		capMS = n
	}
	return def, max(capMS, def)
}

// toolTimeout returns the timeout to write for a call whose own timeout is
// raw, adding wait for the queue. It is never below what Claude Code would
// use without the hook and never above its cap; capped says the cap left
// less than wait for the queue, so queue time comes out of the command's own.
func toolTimeout(raw json.RawMessage, wait time.Duration, e ToolTimeouts) (ms, capMS int64, capped bool) {
	def, capMS := e.limits()
	base := def // what Claude Code would apply to the original call: min(timeout or default, cap)
	var t float64
	if len(raw) > 0 && json.Unmarshal(raw, &t) == nil && t > 0 {
		base = capMS
		if t < float64(capMS) {
			base = int64(t)
		}
	}
	want := base + wait.Milliseconds()
	if want > capMS {
		return capMS, capMS, true
	}
	return want, capMS, false
}

// jsParseInt reads a decimal integer the way JavaScript's parseInt(s, 10)
// does: leading white space, an optional sign, then digits up to the first
// non-digit.
func jsParseInt(s string) (int64, bool) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	end := 0
	if end < len(s) && (s[end] == '+' || s[end] == '-') {
		end++
	}
	start := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == start {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	return n, err == nil
}
