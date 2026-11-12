// Package remote runs work on spare machines: metrics probes, pushing exact
// commits to a bare repo there, and running a job in a temporary worktree.
//
// Everything goes through a Transport, an argv prefix to which ONE shell
// command string is appended. For ssh that is `ssh -o BatchMode=yes host`;
// tests and CI use `sh -c`, which makes the "remote" the local machine and
// lets the full remote path run end to end without an ssh server.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Transport is the argv prefix used to reach a machine.
type Transport []string

// ShellQuote quotes s for a POSIX shell.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./_-", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin quotes and joins argv into one shell command line.
func ShellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = ShellQuote(a)
	}
	return strings.Join(q, " ")
}

// commandLine builds `env K=V... bash -c '<script>' heavyq <args...>`, so the
// script runs under bash regardless of the remote login shell.
func commandLine(env []string, script string, args ...string) string {
	parts := []string{}
	if len(env) > 0 {
		parts = append(parts, "env")
		for _, e := range env {
			parts = append(parts, ShellQuote(e))
		}
	}
	parts = append(parts, "bash", "-c", ShellQuote(script), "heavyq")
	for _, a := range args {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " ")
}

// Command returns an exec.Cmd running script remotely with args and env.
func (t Transport) Command(ctx context.Context, env []string, script string, args ...string) (*exec.Cmd, error) {
	if len(t) == 0 {
		return nil, fmt.Errorf("empty transport")
	}
	argv := append(append([]string{}, t[1:]...), commandLine(env, script, args...))
	return exec.CommandContext(ctx, t[0], argv...), nil
}

// Output runs script remotely with a timeout and returns stdout.
func (t Transport) Output(timeout time.Duration, script string, args ...string) (string, error) {
	return whole(t.output(context.Background(), timeout, false, nil, script, args...))
}

// OutputPartial is Output that also returns, with the error, what the script printed before it failed or
// ran out of time: a script that reports in steps keeps the steps that arrived.
func (t Transport) OutputPartial(timeout time.Duration, script string, args ...string) (string, error) {
	return t.output(context.Background(), timeout, false, nil, script, args...)
}

// outputEnv is Output with env (K=V) set for the script on the remote side.
func (t Transport) outputEnv(timeout time.Duration, env []string, script string, args ...string) (string, error) {
	return whole(t.output(context.Background(), timeout, false, env, script, args...))
}

// whole drops the partial output of a failed run.
func whole(out string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return out, nil
}

// OutputContext is Output that also ends with ctx. The transport runs in a
// session of its own (see isolate): a terminal's ^C reaches heavyq only, which
// ends ctx and reports the signal, and the end of ctx stops the transport
// with everything it started.
func (t Transport) OutputContext(ctx context.Context, timeout time.Duration, script string, args ...string) (string, error) {
	return whole(t.output(ctx, timeout, true, nil, script, args...))
}

func (t Transport) output(parent context.Context, timeout time.Duration, own bool, env []string, script string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd, err := t.Command(ctx, env, script, args...)
	if err != nil {
		return "", err
	}
	cmd.WaitDelay = time.Second
	if own {
		isolate(cmd)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if parent.Err() != nil {
			return out.String(), errors.New("stopped")
		}
		if ctx.Err() != nil {
			return out.String(), fmt.Errorf("timed out after %s", timeout)
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("%s", msg)
	}
	return out.String(), nil
}
