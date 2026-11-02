#!/usr/bin/env python3
"""Check intra-repo links in Markdown files: the target file exists and a #fragment matches a heading.

Usage: .github/scripts/check-md-links.py [ROOT]   (default: the git work tree this file lives in)
External links (http, https, mailto) are not fetched. Fenced code blocks and inline code are ignored when looking for links.
Exit 1 and one line per broken link when anything is wrong.
"""
import re
import sys
from pathlib import Path

SKIP_DIRS = {".git", "plans", ".claude", "node_modules", "dist", "vendor"}
LINK = re.compile(r"(?<!\!)\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)|!\[[^\]]*\]\(([^)\s]+)\)")
HEADING = re.compile(r"^#{1,6}\s+(.*?)\s*#*\s*$")


def slug(title: str) -> str:
    """GitHub's heading anchor: lowercase, drop punctuation except - and _, spaces to dashes."""
    title = re.sub(r"`", "", title.strip().lower())
    title = re.sub(r"[^\w\- ]", "", title)
    return title.replace(" ", "-")


def strip_code(text: str) -> list[tuple[int, str]]:
    """Return (line number, line) for lines outside fenced blocks."""
    out, fence = [], None
    for n, line in enumerate(text.split("\n"), 1):
        m = re.match(r"^\s*(```+|~~~+)", line)
        if m:
            if fence is None:
                fence = m.group(1)[:3]
            elif m.group(1).startswith(fence):
                fence = None
            continue
        if fence is None:
            out.append((n, line))
    return out


def anchors(path: Path, cache: dict) -> set:
    if path not in cache:
        seen, result = {}, set()
        for _, line in strip_code(path.read_text(encoding="utf-8")):
            m = HEADING.match(line)
            if m:
                s = slug(m.group(1))
                k = seen.get(s, 0)
                result.add(s if k == 0 else f"{s}-{k}")
                seen[s] = k + 1
        cache[path] = result
    return cache[path]


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).resolve().parents[2]).resolve()
    cache, bad = {}, []
    for md in sorted(root.rglob("*.md")):
        if SKIP_DIRS & set(md.relative_to(root).parts):
            continue
        for n, line in strip_code(md.read_text(encoding="utf-8")):
            for m in LINK.finditer(re.sub(r"`[^`]*`", "", line)):
                target = m.group(1) or m.group(2)
                if re.match(r"^[a-z][a-z0-9+.-]*:", target):  # http:, https:, mailto:
                    continue
                file_part, _, frag = target.partition("#")
                # Changelog fragments are folded into the root CHANGELOG.md, so
                # their links are written relative to the repo root.
                fragment = md.parent.name == "changelog.d" and md.name != "README.md"
                base = root if fragment else md.parent
                dest = md if file_part == "" else (base / file_part).resolve()
                if not dest.exists():
                    bad.append(f"{md.relative_to(root)}:{n}: missing file {target}")
                elif frag and dest.suffix == ".md" and frag.lower() not in anchors(dest, cache):
                    bad.append(f"{md.relative_to(root)}:{n}: no heading #{frag} in {dest.relative_to(root)}")
    print("\n".join(bad) if bad else "all intra-repo Markdown links resolve")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
