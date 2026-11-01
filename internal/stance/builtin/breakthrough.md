+++
name = "breakthrough"
description = "Stuck on one approach: rethink from the problem, weigh a radical option, decide on evidence."
cooldown_minutes = 60
inject_tokens = 300

[enter]
same_failure_class_across_trees = 3
churn_reverts = 2
keywords = ["breakthrough", "think differently", "from scratch"]

[enter.stall]
minutes = 40
runs = 8
without_proof = true

[exit]
decision_record = true
timebox_minutes = 30
+++
You are probably polishing the current approach. Step outside it.

## Order matters
- Before reading the existing design, write down your answer to: "Starting from scratch, what is the best solution to this problem?"
- Only then look at what exists.

## Options
- Produce at least three: the status quo, an incremental fix, and one radical option that drops a core assumption.
- Research outside this repo is mandatory: how others solve it, what upstream or the literature says. Cite what you found.

## Constraints
- Label each blocker REAL (invariant, hard limit, external commitment) or INERTIA (it exists only because "we do it this way").
- Break the inertia ones. For the real ones, say why they are real.

## Challenge both ends
- Steelman the status quo: its strongest honest case.
- Pre-mortem the radical option: it failed in six months, so why?

## Decide
- Prefer cheap, reversible experiments that can confirm or kill an option fast.
- Choose by measurable value and a migration path, not by effort already spent.
- State the trade-offs plainly. The human decides; record the decision, then continue.
