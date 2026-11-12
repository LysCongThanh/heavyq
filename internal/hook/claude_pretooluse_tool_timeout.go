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

// maxTimerMS is the largest timeout Claude Code accepts at all (a JavaScript
// timer's limit); it clamps every timeout to it.
const maxTimerMS = 2147483647

// toolTimeout returns the timeout to write for a call whose own timeout is
// raw, adding wait for the queue; 0 means leave timeout out. A foreground
// call's timeout is never below what Claude Code would use without the hook
// and never above its cap; capped says the cap left less than wait for the
// queue, so queue time comes out of the command's own. A background call
// (run_in_background: true) has its own, larger limits that the hook cannot
// read: without a timeout it is left to Claude Code's background default,
// with one it gets that timeout plus wait and Claude Code applies its limit.
func toolTimeout(raw json.RawMessage, background bool, wait time.Duration, e ToolTimeouts) (ms, capMS int64, capped bool) {
	var t float64
	explicit := len(raw) > 0 && json.Unmarshal(raw, &t) == nil && t > 0
	if background {
		if !explicit {
			return 0, 0, false
		}
		return int64(min(t+float64(wait.Milliseconds()), maxTimerMS)), 0, false
	}
	def, capMS := e.limits()
	base := def // what Claude Code would apply to the original call: min(timeout or default, cap)
	if explicit {
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

// isTrue says whether a tool_input field is the JSON value true.
func isTrue(raw json.RawMessage) bool {
	var b bool
	return len(raw) > 0 && json.Unmarshal(raw, &b) == nil && b
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
