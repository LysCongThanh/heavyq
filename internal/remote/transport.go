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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := t.Command(ctx, nil, script, args...)
	if err != nil {
		return "", err
	}
	cmd.WaitDelay = time.Second
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out after %s", timeout)
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return out.String(), nil
}
