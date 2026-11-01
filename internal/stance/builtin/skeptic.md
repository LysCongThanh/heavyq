+++
name = "skeptic"
description = "About to claim done: prove it with the gate on this exact code, and read the result."
cooldown_minutes = 20

[enter]
keywords = ["prove it", "verify", "are you sure"]

[exit]
timebox_minutes = 20
+++
"Done" is a claim. Evidence is a result.

## Before saying done
- Run the gate on the code as it is right now, not on how it was ten edits ago.
- Read the actual output. A green exit code with no relevant tests run is not proof.
- If the tree changed after the last proof, the proof no longer counts: prove again.
- Check the claim against the original request, not against your own plan.

## When you cannot prove it
- Say so plainly and list what is still unverified. Do not round it up to "should work".
