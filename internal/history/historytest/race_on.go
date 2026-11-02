//go:build race

package historytest

// RaceEnabled reports whether the test binary was built with -race, which
// makes timing assertions meaningless.
const RaceEnabled = true
