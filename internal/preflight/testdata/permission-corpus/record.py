#!/usr/bin/env python3
"""Re-record corpus.json against a live headless Claude Code.

    python3 -I internal/preflight/testdata/permission-corpus/record.py [--model haiku] [ruleset-name ...]

For every ruleset in the corpus it writes a settings file in a temp directory, asks Claude Code
(`claude -p --permission-prompts none --output-format stream-json`) to call the Bash tool once per
case command, and reads what happened: a command that ran is "allow"; a command that shows up in a
`permission_denied` event (and in `permission_denials` of the final result) is "refuse", which with
prompts off covers a deny rule, an ask rule and "no allow rule matched" alike. The decision and the
event's `decision_reason_type` are written back into the corpus together with the Claude Code version.

The commands are harmless in an empty directory (`go test` with no go.mod fails, `git push` is not in a
repository). A hook rewrite is simulated with a stub PreToolUse hook and a stub `heavyq` on PATH, so
no real queue is touched. Needs a logged-in `claude`; a run costs a fraction of a cent per ruleset.
"""
import argparse
import datetime
import json
import os
import stat
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CORPUS = os.path.join(HERE, "corpus.json")

# The same rewrite the heavyq hook does for `go test`: priority 4, lane api, a 300 s wait.
HOOK_PY = r"""
import json, sys
d = json.load(sys.stdin)
cmd = d.get("tool_input", {}).get("command", "")
if not cmd.startswith("go test"):
    sys.exit(0)
out = {"hookEventName": "PreToolUse",
       "updatedInput": dict(d["tool_input"], command="heavyq 4 --lane api --max-wait 300s -- " + cmd)}
if sys.argv[1:] == ["allow"]:
    out["permissionDecision"] = "allow"
print(json.dumps({"hookSpecificOutput": out}))
"""

PROMPT = (
    "Call the Bash tool once for each command below, in order, one tool call per command, with the "
    "command exactly as written (every character, no changes, no wrapping, no extra flags). Do not "
    "retry a command that fails or is refused. A line that contains &&, ||, ; or | is ONE command: pass the whole line in one call, never split it. Do not explain. The commands, one per line:\n"
)


def write_exec(path, text):
    with open(path, "w") as f:
        f.write(text)
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR)


def record(rs, model, work):
    perms = {k: rs["permissions"].get(k, []) for k in ("allow", "deny", "ask")}
    settings = {"permissions": perms}
    mode = rs.get("hook", "none")
    if mode != "none":
        hook = os.path.join(work, "hook.py")
        with open(hook, "w") as f:
            f.write(HOOK_PY)
        arg = "allow" if mode == "rewrite-allow" else "pass"
        settings["hooks"] = {"PreToolUse": [{"matcher": "Bash", "hooks": [
            {"type": "command", "command": f"python3 -I {hook} {arg}"}]}]}
    sfile = os.path.join(work, "settings.json")
    with open(sfile, "w") as f:
        json.dump(settings, f)
    bindir = os.path.join(work, "bin")
    os.makedirs(bindir)
    write_exec(os.path.join(bindir, "heavyq"), "#!/bin/sh\necho stub heavyq \"$@\"\n")
    cmds = [c["command"] for c in rs["cases"]]
    env = dict(os.environ, PATH=bindir + os.pathsep + os.environ["PATH"])
    # Project settings are ignored until the workspace is trusted, which needs an interactive
    # session; --settings is not subject to that. --setting-sources local keeps the user's own
    # rules and hooks out of the recording.
    proc = subprocess.run(
        ["claude", "-p", "--model", model, "--setting-sources", "local", "--settings", sfile,
         "--permission-prompts", "none", "--output-format", "stream-json", "--verbose",
         "--no-session-persistence", "--max-budget-usd", "0.5"],
        input=PROMPT + "\n".join(cmds) + "\n", capture_output=True, text=True, cwd=work, env=env, timeout=600)
    calls, denied, version = {}, {}, ""
    for line in proc.stdout.splitlines():
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("type") == "system" and ev.get("subtype") == "init":
            version = ev.get("claude_code_version", "")
        if ev.get("type") == "system" and ev.get("subtype") == "permission_denied":
            denied[ev["tool_use_id"]] = ev.get("decision_reason_type", "")
        if ev.get("type") == "assistant":
            for blk in ev["message"]["content"]:
                if blk.get("type") == "tool_use" and blk.get("name") == "Bash":
                    calls[blk["id"]] = blk["input"]["command"]
        if ev.get("type") == "result":
            for d in ev.get("permission_denials", []):
                denied.setdefault(d["tool_use_id"], "")
    if not version:
        sys.exit(f"{rs['name']}: no init event; stderr: {proc.stderr[:300]}")
    seen = {}
    for tid, cmd in calls.items():
        seen.setdefault(cmd, tid)
    for c in rs["cases"]:
        tid = seen.get(c["command"])
        if tid is None:
            sys.exit(f"{rs['name']}: the model did not run {c['command']!r} as written (ran {sorted(seen)})")
        c["decision"] = "refuse" if tid in denied else "allow"
        c["reason"] = denied.get(tid, "")
    return version


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="haiku")
    ap.add_argument("only", nargs="*")
    args = ap.parse_args()
    with open(CORPUS) as f:
        corpus = json.load(f)
    version = ""
    for rs in corpus["rulesets"]:
        if args.only and rs["name"] not in args.only:
            continue
        with tempfile.TemporaryDirectory() as work:
            version = record(rs, args.model, work)
        print(f"recorded {rs['name']}: " + ", ".join(f"{c['decision']}" for c in rs["cases"]))
    if version:
        corpus["claude_code_version"] = version
        corpus["recorded"] = datetime.date.today().isoformat()
    with open(CORPUS, "w") as f:
        json.dump(corpus, f, indent=1)
        f.write("\n")


if __name__ == "__main__":
    main()
