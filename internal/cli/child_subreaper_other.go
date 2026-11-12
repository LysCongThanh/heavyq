//go:build !linux

package cli

// becomeSubreaper is Linux-only (macOS has no PR_SET_CHILD_SUBREAPER);
// elsewhere a cancel reaches the direct child, the descendants visible at that
// moment and those an earlier scan saw, see processTree.
func becomeSubreaper() bool { return false }
