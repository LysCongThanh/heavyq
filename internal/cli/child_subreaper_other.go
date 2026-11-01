//go:build !linux

package cli

// becomeSubreaper is Linux-only; elsewhere a cancel reaches the direct child
// and the descendants visible at that moment.
func becomeSubreaper() bool { return false }
