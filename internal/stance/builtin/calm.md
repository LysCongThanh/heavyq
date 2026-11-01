+++
name = "calm"
description = "Failure looks like noise (flaky or load-induced): do not change code for it."
next = "re-run once, unchanged, with heavyq gate run"
cooldown_minutes = 20

[enter]
keywords = ["flaky", "calm down"]

[exit]
timebox_minutes = 15
+++
Not every red run is a bug in your change.
<!-- core -->
- Check whether the failure touches code you changed.
- Look at machine load before reading anything into a timeout.
- No sleeps, retries or loosened assertions for a failure you do not understand.
<!-- /core -->

## Before editing anything
- Look at the failure: does it touch code you changed, or does it time out, hang or fail differently on each run?
- Check machine load. A slow, crowded host produces timeouts that say nothing about correctness.
- Re-run once, unchanged. Same failure in the same place means it is real; a different result means it is noise.

## Rules
- Do not add sleeps, retries or loosened assertions to quiet a failure you have not understood.
- If it is noise, note the test name and move on. If it is real, treat it as a normal failure and fix the cause.
