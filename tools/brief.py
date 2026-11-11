#!/usr/bin/env python3
"""The reviewer's brief for a stage: what it asks, what the reference built, what to look for in
review, and the Framewise assignment material the stage stands for.

    tools/brief.py STAGE_SLUG [--full] [--diff]
    tools/brief.py --list

--full prints the stage's assignment weeks whole; by default each shows what it adds, its task,
when it is done and its reading list. --diff appends the reference
diff itself. --list prints every stage with its Framewise weeks and review marks.

The assignment material comes from the framewise repo (ASSIGNMENTS.md and TUTOR.md): set
FRAMEWISE_REPO, or clone it next to this repo or next to the directory you run from. Without it the
brief says so and prints the rest.

Standard library only, so it runs wherever the course's shell tools do.
"""

import argparse
import ast
import os
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


def weeks_by_slug() -> dict[str, list[int]]:
    """Framewise weeks per stage, from the README's stage table."""
    rows = table_rows((COURSE / "README.md").read_text(), "| # | Slug |")
    return {r[1]: [int(w) for w in re.findall(r"\d+", r[4])] for r in rows if len(r) >= 5}


def review_rows() -> dict[str, tuple[bool, str]]:
    """(starred, what to look for) per stage, from REVIEWING.md."""
    rows = table_rows((COURSE / "REVIEWING.md").read_text(), "| Stage |")
    out = {}
    for r in rows:
        if len(r) >= 2:
            starred = "★" in r[0]
            out[r[0].replace("★", "").strip()] = (starred, r[1])
    return out


def framewise_repo() -> Path | None:
    candidates = [os.environ.get("FRAMEWISE_REPO", ""), COURSE.parent / "framewise", Path.cwd().parent / "framewise"]
    for c in candidates:
        if c and (Path(c) / "ASSIGNMENTS.md").is_file():
            return Path(c)
    return None


def week_sections(assignments: str) -> dict[int, str]:
    """'### Week N: ...' sections of ASSIGNMENTS.md, heading included."""
    out = {}
    parts = re.split(r"(?m)^(?=### Week \d+:)", assignments)
    for part in parts:
        if m := re.match(r"### Week (\d+):", part):
            # A week ends at the next heading of any level up to ###.
            body = re.split(r"(?m)^(?=#{1,3} )", part[len(m.group(0)) :], maxsplit=1)[0]
            out[int(m.group(1))] = (part[: len(m.group(0))] + body).rstrip()
    return out


def fields(section: str) -> dict[str, str]:
    """A week section's **Name:** blocks, each up to the next one."""
    out = {}
    for m in re.finditer(r"(?ms)^\*\*([^*\n]+):\*\*(.*?)(?=^\*\*[^*\n]+:\*\*|\Z)", section):
        out[m.group(1)] = m.group(2).strip()
    return out


def reading(block: str) -> list[str]:
    """The titles in a numbered reading list, one per item."""
    items = []
    for line in block.splitlines():
        if m := re.match(r"\d+\. (.*)", line):
            titles = re.findall(r"\[([^\]]+)\]", m.group(1))
            items.append(", ".join(titles) if titles else m.group(1))
    return items


def compact_week(section: str) -> list[str]:
    """What the week adds, what to read, what it asks and when it is done."""
    f = fields(section)
    out = []
    for name in ("Gains", "Task", "Done when"):
        if f.get(name):
            sep = "\n" if f[name].startswith("- ") else " "
            out += [f"{name}:{sep}{f[name]}", ""]
    if titles := reading(f.get("Read, in order", "")):
        out += ["Reading:"] + [f"  {i}. {t}" for i, t in enumerate(titles, 1)] + [""]
    return out


def tutor_rows(tutor: str) -> dict[int, list[str]]:
    rows = table_rows(tutor, "| Week |")
    return {int(r[0]): r for r in rows if r and r[0].isdigit()}


def description(slug: str, n: int) -> tuple[Path, str, str]:
    """The stage text's path, its opening (before the first heading) and its 'why it broke'."""
    path = COURSE / "stage_descriptions" / f"base-{n:02d}-{slug}.md"
    text = path.read_text()
    opening = re.split(r"(?m)^#{1,4} ", text, maxsplit=1)[0].strip()
    m = re.search(r"(?ms)^### Why your design just broke\s*\n(.*?)(?=^### |\Z)", text)
    return path, opening, (m.group(1).strip() if m else "")


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


def brief(slug: str, full: bool, with_diff: bool) -> str:
    all_stages = stages()
    slugs = [s for s, _ in all_stages]
    if slug not in slugs:
        sys.exit(f"unknown stage: {slug} (have: {' '.join(slugs)})")
    n = slugs.index(slug) + 1
    name = all_stages[n - 1][1]
    weeks = weeks_by_slug()
    starred, look_for = review_rows().get(slug, (False, ""))
    stage_weeks = weeks.get(slug, [])

    out = []
    title = f"# Stage {n}/{len(slugs)}: {name} ({slug}){' ★' if starred else ''}"
    out += [title, ""]
    if stage_weeks:
        out += [f"Framewise week{'s' if len(stage_weeks) > 1 else ''} {', '.join(map(str, stage_weeks))}.", ""]

    path, opening, why = description(slug, n)
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

    out += ["## The assignment", ""]
    repo = framewise_repo()
    if not stage_weeks:
        out += ["  This stage has no Framewise week.", ""]
    elif repo is None:
        out += [
            "  framewise/ASSIGNMENTS.md not found: set FRAMEWISE_REPO, or clone framewise next to this",
            "  course or next to the directory you run from.",
            "",
        ]
    else:
        sections = week_sections((repo / "ASSIGNMENTS.md").read_text())
        tutor = tutor_rows((repo / "TUTOR.md").read_text()) if (repo / "TUTOR.md").is_file() else {}
        for w in stage_weeks:
            section = sections.get(w)
            if section is None:
                out += [f"  Week {w}: not in ASSIGNMENTS.md", ""]
                continue
            heading = section.splitlines()[0].lstrip("# ").strip()
            out += [f"### {heading}", ""]
            out += [section.split("\n", 1)[1].strip(), ""] if full else compact_week(section)
            if row := tutor.get(w):
                out += [f"Tutor's review list: {row[2]}"]
                out += [f"Common mistakes: {row[3]}", ""] if len(row) > 3 else [""]
        out += [f"From {repo}/ASSIGNMENTS.md and TUTOR.md{'' if full else '; --full for whole weeks'}.", ""]

    if with_diff:
        out += ["## The reference diff", ""]
        for d in sorted(diff_dir.rglob("*.diff")):
            out += [d.read_text(errors="replace").rstrip(), ""]
    return "\n".join(out).rstrip() + "\n"


def listing() -> str:
    weeks = weeks_by_slug()
    review = review_rows()
    out = []
    for i, (slug, name) in enumerate(stages(), 1):
        starred = review.get(slug, (False, ""))[0]
        w = ",".join(map(str, weeks.get(slug, [])))
        out.append(f"{i:3d}  {slug}  {'★' if starred else ' '}  {name:<32} week {w}")
    return "\n".join(out) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("slug", nargs="?")
    parser.add_argument("--full", action="store_true", help="print every assignment week in full")
    parser.add_argument("--diff", action="store_true", help="append the reference diff")
    parser.add_argument("--list", action="store_true", help="every stage, its weeks and ★")
    args = parser.parse_args()
    if args.list or not args.slug:
        sys.stdout.write(listing())
        return
    sys.stdout.write(brief(args.slug, args.full, args.diff))


if __name__ == "__main__":
    try:
        main()
    except BrokenPipeError:
        pass
