#!/usr/bin/env python3
"""The reviewer's brief for a stage: what it asks, what the reference built, what to look for in
review and what the stage points the learner to read.

    tools/brief.py STAGE_SLUG [--diff]
    tools/brief.py --list

--diff appends the reference diff itself. --list prints every stage, with ★ on the ones REVIEWING.md
suggests doing yourself.

Standard library only, so it runs wherever the course's shell tools do.
"""

import argparse
import ast
import re
import sys
from pathlib import Path

COURSE = Path(__file__).resolve().parent.parent
SOLUTIONS = COURSE / "solutions" / "python"
# What the learner writes (the same list as tools/compile.sh and tools/fast_track.sh).
LEARNER_PATHS = ["app", "tests", ".gitlab-ci.yml", "Containerfile", ".containerignore", "deploy"]


def stages() -> list[tuple[str, str]]:
    """(slug, name) in course order, from course-definition.yml."""
    out, slug = [], None
    in_stages = False
    for line in (COURSE / "course-definition.yml").read_text().splitlines():
        if line.startswith("stages:"):
            in_stages = True
        elif in_stages and (m := re.match(r'^  - slug: *"?([^"]+)"?', line)):
            slug = m.group(1)
        elif in_stages and slug and (m := re.match(r'^    name: *"?(.*?)"?$', line)):
            out.append((slug, m.group(1)))
            slug = None
    return out


def table_rows(text: str, header_start: str) -> list[list[str]]:
    """The body rows of the first Markdown table whose header starts with header_start."""
    lines = text.splitlines()
    for i, line in enumerate(lines):
        if line.startswith(header_start):
            rows = []
            for row in lines[i + 2 :]:
                if not row.startswith("|"):
                    break
                rows.append([c.strip() for c in row.strip().strip("|").split("|")])
            return rows
    return []


def review_rows() -> dict[str, tuple[bool, str]]:
    """(starred, what to look for) per stage, from REVIEWING.md."""
    rows = table_rows((COURSE / "REVIEWING.md").read_text(), "| Stage |")
    out = {}
    for r in rows:
        if len(r) >= 2:
            starred = "★" in r[0]
            out[r[0].replace("★", "").strip()] = (starred, r[1])
    return out


def section(text: str, heading: str) -> str:
    """The body of a ### section of a stage text."""
    m = re.search(rf"(?ms)^### {re.escape(heading)}\s*\n(.*?)(?=^### |\Z)", text)
    return m.group(1).strip() if m else ""


def description(slug: str, n: int) -> tuple[Path, str, str, str]:
    """The stage text's path, its opening (before the first heading), its 'why it broke' and its
    pointers."""
    path = COURSE / "stage_descriptions" / f"base-{n:02d}-{slug}.md"
    text = path.read_text()
    opening = re.split(r"(?m)^#{1,4} ", text, maxsplit=1)[0].strip()
    return path, opening, section(text, "Why your design just broke"), section(text, "Pointers")


def diff_files(n: int, slug: str) -> list[tuple[str, int, int, bool]]:
    """(file, lines added, lines removed, new file) for each file the stage's reference changed."""
    root = SOLUTIONS / f"{n:02d}-{slug}" / "diff"
    out = []
    for d in sorted(root.rglob("*.diff")):
        added = removed = 0
        new = False
        for line in d.read_text(errors="replace").splitlines():
            if line.startswith(("+++", "---")):
                continue
            if line.startswith("@@ -0,0 "):
                new = True
            elif line.startswith("+"):
                added += 1
            elif line.startswith("-"):
                removed += 1
        out.append((str(d.relative_to(root))[: -len(".diff")], added, removed, new))
    return out


def summary_line(path: Path) -> str:
    """A file's one-line purpose: a Python module's docstring, or a leading comment."""
    try:
        text = path.read_text()
    except (UnicodeDecodeError, OSError):
        return ""
    if path.suffix == ".py":
        try:
            doc = ast.get_docstring(ast.parse(text))
        except SyntaxError:
            doc = None
        if doc:
            return doc.strip().splitlines()[0]
        return ""
    for line in text.splitlines()[:5]:
        line = line.strip()
        if line.startswith("#") and not line.startswith("#!") and len(line) > 2:
            return line.lstrip("# ").strip()
    return ""


def code_map(n: int, slug: str, changed: dict[str, bool]) -> list[str]:
    """Every file the learner has written by the end of the stage, with what it is for."""
    code = SOLUTIONS / f"{n:02d}-{slug}" / "code"
    files = []
    for p in LEARNER_PATHS:
        path = code / p
        if path.is_file():
            files.append(path)
        elif path.is_dir():
            files += [f for f in sorted(path.rglob("*")) if f.is_file() and "__pycache__" not in f.parts]
    lines = []
    for f in files:
        rel = str(f.relative_to(code))
        if rel.endswith("__init__.py") and not f.read_text().strip():
            continue
        mark = "+" if changed.get(rel) else "~" if rel in changed else " "
        about = summary_line(f)
        lines.append(f"  {mark} {rel}" + (f"  — {about}" if about else ""))
    return lines


def wrap(text: str, indent: str = "") -> str:
    import textwrap

    return "\n\n".join(
        textwrap.fill(p, width=100, initial_indent=indent, subsequent_indent=indent) if p.strip() else ""
        for p in text.split("\n\n")
    )


def brief(slug: str, with_diff: bool) -> str:
    all_stages = stages()
    slugs = [s for s, _ in all_stages]
    if slug not in slugs:
        sys.exit(f"unknown stage: {slug} (have: {' '.join(slugs)})")
    n = slugs.index(slug) + 1
    name = all_stages[n - 1][1]
    starred, look_for = review_rows().get(slug, (False, ""))

    out = [f"# Stage {n}/{len(slugs)}: {name} ({slug}){' ★' if starred else ''}", ""]

    path, opening, why, pointers = description(slug, n)
    out += ["## The task", "", wrap(opening), ""]
    if why:
        out += ["What breaks:", "", wrap(why, "  "), ""]
    out += [f"Full text: {path}  (or ./test.sh --show {slug})", ""]

    changes = diff_files(n, slug)
    changed = {f: new for f, _, _, new in changes}
    out += ["## What the reference built", ""]
    if changes:
        width = max(len(f) for f, *_ in changes)
        for f, a, r, new in changes:
            out.append(f"  {f.ljust(width)}  {'new' if new else '   '}  +{a} -{r}")
    else:
        out.append("  (no changes to the learner's files)")
    diff_dir = SOLUTIONS / f"{n:02d}-{slug}" / "diff"
    out += ["", f"Diff: {diff_dir}/  (tools/brief.py {slug} --diff to print it)", ""]
    out += ["The learner's code after this stage (+ new, ~ changed here):", ""]
    out += code_map(n, slug, changed)
    out.append("")

    out += ["## In review", ""]
    if look_for:
        out += [wrap(look_for, "  "), ""]
    if starred:
        out += ["  ★ REVIEWING.md suggests doing this one yourself rather than reading the diff.", ""]
    out += ["  Always: basedpyright and ruff clean, unit tests for pure logic, profiles on performance PRs.", ""]

    if pointers:
        out += ["## What the learner reads", "", pointers, ""]

    if with_diff:
        out += ["## The reference diff", ""]
        for d in sorted(diff_dir.rglob("*.diff")):
            out += [d.read_text(errors="replace").rstrip(), ""]
    return "\n".join(out).rstrip() + "\n"


def listing() -> str:
    review = review_rows()
    out = []
    for i, (slug, name) in enumerate(stages(), 1):
        starred = review.get(slug, (False, ""))[0]
        out.append(f"{i:3d}  {slug}  {'★' if starred else ' '}  {name}")
    return "\n".join(out) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("slug", nargs="?")
    parser.add_argument("--diff", action="store_true", help="append the reference diff")
    parser.add_argument("--list", action="store_true", help="every stage, ★ on those worth doing yourself")
    args = parser.parse_args()
    if args.list or not args.slug:
        sys.stdout.write(listing())
        return
    sys.stdout.write(brief(args.slug, args.diff))


if __name__ == "__main__":
    try:
        main()
    except BrokenPipeError:
        pass
