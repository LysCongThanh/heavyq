package cli

// remoteNotFound follows a remote run that exited 127, the shell's "command
// not found": what was cached about that remote's tools is dropped, so the next
// placement asks it again instead of trusting an answer the 127 disproved, and a
// person gets one line naming the remote and its env_path (an agent reads the
// same in the digest's next line, see failNext).
func (r *run) remoteNotFound(machine string) {
	_ = toolCache().Forget(machine) // fail-open: a stale answer costs one more 127, not a wrong result
	m, _ := r.cfg.Machine(machine)
	r.note("exit 127 on %s: a command was not found on the PATH its jobs get (env_path %q of [[remote]] %s in %s); "+
		"install it there or add its directory to env_path (`heavyq doctor` lists each remote's tools)",
		machine, m.EnvPath, machine, cfgName(r.cfg))
}
