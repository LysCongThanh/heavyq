package stance

import "os"

// EnvStances is the environment variable that turns every stance off for the
// process: HEAVYQ_STANCES=off.
const EnvStances = "HEAVYQ_STANCES"

// Disabled reports whether HEAVYQ_STANCES=off is set.
func Disabled() bool { return os.Getenv(EnvStances) == "off" }

// ActiveFor is what a SessionStart(compact) brief calls to put the stance back
// after the context was compacted: the compact render (Render, with the
// evidence it entered on) of the stance the lane is in right now, or ok=false
// when the lane is in none.
//
// It is read-only and cheap (one lane file, the stance files and the trust
// list), uses the process's directory to find the repository and the real
// user directories, and fails quiet: any problem, a muted lane, a stance past
// its time box, one that was deleted, or one from an untrusted repository
// (trust changed since it entered) is ok=false and an empty string. The text is
// at most inject_tokens x 4 bytes, so it fits the brief's budget by itself.
func ActiveFor(lane string) (render string, ok bool) {
	cwd, err := os.Getwd()
	if err != nil || Disabled() {
		return "", false
	}
	return DefaultEnv(cwd).ActiveFor(cwd, lane)
}

// ActiveFor is the function of the same name over an explicit Env and directory.
func (e Env) ActiveFor(cwd, lane string) (render string, ok bool) {
	set, err := e.LoadSet(cwd)
	if err != nil {
		return "", false
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return "", false
	}
	st, err := store.Load(lane)
	if err != nil || st.Muted || st.Active == nil {
		return "", false
	}
	if !st.Active.Expires.IsZero() && !e.now().Before(st.Active.Expires) {
		return "", false
	}
	s, found := Find(set.Injectable, st.Active.Stance)
	if !found {
		return "", false
	}
	return Render(s, st.Active.Why), true
}
