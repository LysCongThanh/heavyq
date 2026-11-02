---
name: heavyq
description: Use when running or checking heavy commands (tests, builds, lint gates) in a repo where the heavyq binary is installed. Explains how to ask whether this exact code is already proven (heavyq gate status), run only what is unproven (heavyq gate run), find out why a command waits (heavyq why), and read the one-page agent card (heavyq help agents). Covers exit codes 75 (queue busy, retry later) and 78 (cancelled by an operator), and the rule never to bypass heavyq.
---

# heavyq

heavyq queues heavy shell commands by priority, gates them on RAM and load, and records which code each
run proved. This plugin's hook already routes heavy Bash commands through it; you rarely type it yourself.

## When to use

- **Before a test, build or lint run:** `heavyq gate status`. Exit 0 means this exact code is proven: do
  not run it again.
- **To prove the code:** `heavyq gate run` (or `heavyq gate run <name>`). It runs only what is unproven,
  through the queue, and prints a short digest with one `next:` line. Do what it says.
- **When a command waits or is slow:** `heavyq why` (or `heavyq why <lane>`) says who waits and why.
- **When unsure of a flag or an output:** `heavyq help agents` prints a 200-token card. Read it once.

## Exit codes

- `75`: the queue stayed busy past the wait limit. Retry later. Never bypass heavyq.
- `78`: an operator cancelled the run. Ask before retrying.
- `76` / `77`: the remote machine is down or failed. Retry, or add `--remote local`.
- `2`: usage or setup error; nothing ran. Read the message.

## Rules

- Never bypass heavyq: no `sh -c`, `env`, absolute paths or other tricks to run a queued command directly.
- A `REUSED` digest means the same command already passed on this exact tree. Do not re-run it.
- If `heavyq` is not installed, its commands fail with "not found". Tell the user to install it
  (`go install github.com/LysCongThanh/heavyq@latest`) instead of working around it.
