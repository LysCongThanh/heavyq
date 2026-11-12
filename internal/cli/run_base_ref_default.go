package cli

import (
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// effectiveBaseRef is the --base-ref a run in the repository at dir really gets, and the one whose commit
// joins its run id. asked is what --base-ref or queue.base_ref said:
//
//   - a remote/branch ref: that ref, as given;
//   - config.BaseRefNone: no base ref at all;
//   - nothing: origin's default branch (refs/remotes/origin/HEAD, else origin/main) when the repository has
//     one, so a command that diffs against origin/main also finds it on a remote machine; "" when it has not.
//
// Every place that computes a run id (a run, the cache key, `gate status`) goes through here, so a proof
// recorded by a run is the one `gate status` looks for, wherever the run executed. An unusable name
// (anything --base-ref would refuse) is no default: it fails closed to "no base ref".
func effectiveBaseRef(dir, asked string) string {
	switch asked {
	case config.BaseRefNone:
		return ""
	case "":
		if dir == "" {
			return ""
		}
		if ref, ok := gitutil.OriginDefaultRef(dir); ok && config.ValidBaseRef(ref) {
			return ref
		}
		return ""
	}
	return asked
}
