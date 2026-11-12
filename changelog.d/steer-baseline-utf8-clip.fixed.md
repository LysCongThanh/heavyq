**The Stop hook no longer blames a session for a failure that was red before it started, when the command is long
and non-ASCII.** The session baseline clips each failing command to 300 bytes; the cut could split a multi-byte
character (an em dash, accented or Vietnamese text in a long `sh -c` gate script), and the session file then stored it
with a replacement character, so the command stopped matching at Stop. The session was blocked or continued over it and
the brief lost "not yours". The clip now ends on a character boundary and drops stray non-UTF-8 bytes, on both sides.
