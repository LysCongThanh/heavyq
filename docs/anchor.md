# Anchor: goal and items in the repository

An **anchor** is a short, reviewable list of what is left to do, kept in git. It holds a goal and checklist
items, and each item can say how it is proven. The default file is `.heavyq/anchor.md`.

> Status: this page describes the file format and the reader/rewriter (`internal/anchor`). `heavyq item`
> ([items.md](items.md)) uses them; `heavyq brief` and the stop gate ([steer.md](steer.md)) read it too.

## Format

```markdown
# Goal: ship login v2

- [ ] login-api: POST /login returns a token  `proof: gate:login-api`
- [ ] ui: login form  `proof: gate:web`  `needs: design sign-off`
- [x] docs: write the guide
  - [ ] docs-api: document the endpoints
```

- **Goal**: the first level-1 heading that starts with `Goal:` (case-insensitive). The text after it is the
  goal. Without one the goal is empty and the parser warns.
- **Items**: any `- [ ]`, `- [x]`, `* [ ]` or `+ [X]` line outside a fenced code block. Indentation (spaces or
  tabs) is allowed and every nested line is its own item.
- **Id**: `<id>: <text>` at the start of the text. An id is 1-64 characters of `a-z 0-9 . _ -`, starting with
  a letter or digit. Emphasis or code around it is fine: `**id**: text`, `**id:** text`, `_id_: text`,
  `` `id`: text ``. A space (or the end of the line) must follow the colon.
- **Without a valid id** the item gets a slug of its text (`Write the docs` becomes `write-the-docs`; text with
  non-ASCII letters gets a short digest suffix) and the parser warns. Give items explicit ids if other
  tools refer to them.
- **Duplicate ids**: the first keeps its id; later ones get `-2`, `-3`, ... and a warning.
- **`proof:`**: an inline code span, anywhere on the line. **`` `proof: gate:web` `` is the recommended form**: it
  names a gate in `heavyq.toml`, whose commands an agent (and any harness that vets commands) can read, and
  `heavyq item done <id> --run --remote <r>` or `heavyq gate run <name> --remote <r>` runs it with nothing to
  quote. `` `proof: go test ./...` `` is a command (run with `sh -c`, pipes are fine) for a quick one-off;
  `heavyq preflight` warns when such a proof needs a shell (a pipe, quotes), because a harness that vets
  commands refuses `heavyq ... -- sh -c '...'` typed by an agent, and suggests moving it to a gate (#123).
  Without a proof the item cannot be proven, only ticked by hand. The first `proof:` span wins.
- **`needs:`**: an inline code span, `` `needs: design sign-off` ``. It marks what a human must provide.
- Everything else (prose, headings, other code spans) is kept as it is and ignored. Checkboxes inside code
  fences never count.

The parser tolerates CRLF line endings, a UTF-8 BOM and tabs. It never fails on odd markdown; it reports a
list of warnings (`path:line: message`) instead.

## Extra sources (read-only)

`heavyq.toml` can add existing plans or docs as read-only sources:

```toml
[anchor]
file  = ".heavyq/anchor.md"                      # optional; this is the default; one .md file, never a directory
extra = ["plans/*/phase-*.md", "docs/**/*.md"]   # globs relative to the repository root
```

Globs use `path.Match` rules plus `**` for any number of directories. `.git` is not searched, and symlinks
that lead outside the repository are ignored. Every checkbox in a matched file becomes an item with
`ReadOnly = true`:

- The id is `<file-slug>/<n>-<text-slug>`: the slug of the repository-relative path (without `.md`), the
  ordinal `n` among items of that file with the same text slug, and the slug of the text. It does not depend on
  line numbers, so adding or moving unrelated lines never renames an item. Editing an item's text does.
- `id:` prefixes are not read; the whole text (minus `proof:` and `needs:` spans) is the text.
- An inline `proof:` is honoured, like in the anchor file.
- heavyq never rewrites these files.

## Limits

At most 2000 items per load and 1 MiB per file. A larger file is skipped with a warning; items beyond the cap
are dropped with a warning. Missing files are fine.

## Rewriting

`anchor.SetChecked(path, id, checked)` flips only the box character of one item in the anchor file. Every
other byte stays: line endings, BOM, trailing spaces. The file is replaced atomically (temp file and rename,
permission bits kept). It refuses ids that do not exist, items from extra sources, symlinks, and files over
1 MiB. It re-reads the file just before writing and refuses if the item's line changed in the meantime
(`ErrConcurrent`; retry). Setting the state an item already has writes nothing.

Writers are not serialised by this package. Two processes flipping different items in the same instant can
lose one update. `heavyq item done` and `heavyq item sync` therefore tick under the lease store's directory lock; they do so only
after it has proof, and the status of an item is derived from proofs, never from the box.
