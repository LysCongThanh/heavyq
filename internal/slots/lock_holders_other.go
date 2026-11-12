//go:build !linux && !darwin

package slots

// heldByAnyone has no lock-free way to look on this system; Busy probes.
func heldByAnyone(string) (busy, known bool) { return false, false }
