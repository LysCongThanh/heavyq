+++
name = "surgeon"
description = "The diff is growing beyond the task: cut it back to the minimal change."
next = "git diff --stat, then revert what this task does not need"
cooldown_minutes = 30

[enter]
keywords = ["minimal change", "scope creep"]

[exit]
timebox_minutes = 20
+++
The change has grown past what the task needs. Shrink it.
<!-- core -->
- Tie every touched file to the task in one sentence, or revert it.
- Undo drive-by renames, reformatting and cleanups.
- Genuinely large fix? Ask to re-scope the item instead of smuggling it in.
<!-- /core -->

## Cut back
- List every file you touched and tie each one to the task in one sentence. If you cannot, revert it.
- Undo drive-by renames, reformatting and "while I am here" cleanups. They can be their own change later.
- Keep only what makes this item work and what its test requires.

## Then
- Re-run the gate on the smaller diff; a smaller change that proves the item beats a larger one that might.
- If the real fix is genuinely large, say so and ask for the item to be re-scoped rather than smuggling it in.
