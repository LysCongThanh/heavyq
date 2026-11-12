**`heavyq top` no longer crashes or overflows a line on text with invalid UTF-8.** Cutting text to the terminal
width counted an invalid byte (for example a `needs:` mark saved in Latin-1 in an anchor file) as three bytes
instead of one. At some widths the line kept two bytes past its budget or split the next character, and when
the byte sat near the end of the text, `top` panicked. Each invalid byte is now cut as one byte and one cell.
