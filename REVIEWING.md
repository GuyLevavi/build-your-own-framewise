# Reviewing Framewise

For whoever reviews a learner's work on this course. The tester decides whether a stage passes;
your review decides whether the code is good. This file has the fast track for learning the course
yourself, and what to look for at each stage.

## The fast track

You don't need to build all 25 stages to review them. For each stage:

```sh
tools/fast_track.sh wk3 ~/fw-review          # app/ becomes the reference solution of wk2
less stage_descriptions/base-11-wk3.md        # the task
tools/run_tests.sh ~/fw-review wk3            # fails: this is what the learner sees
# implement it yourself, or at least sketch it; then:
less solutions/python/11-wk3/diff/app/*.diff  # what the reference changed
tools/run_tests.sh solutions/python/11-wk3/code wk3
```

Doing it for real on the stages marked ★ below and reading the diff for the rest takes about two
days, and covers everything a learner hits. `tools/verify_course.sh` proves the stage chain itself
(each solution passes its stages, and the one before it fails).

## Per stage

Each stage's text ends with "Why your design just broke": that's the refactor to look for. Beyond
passing, check:

| Stage | Look for |
| --- | --- |
| pb1 ★ | A real CLI (argparse/typer), JSON on stdout only, nothing at import time. |
| pb2 | One error type for user errors, caught once in `main`; no tracebacks for bad input; exit codes. |
| an1 | `detect` called once per frame; segments logic separate from video reading and unit-tested. |
| an2 ★ | Frames streamed through a generator, not collected. Ask them to show the memory with Scalene before/after. |
| an3 | Pydantic (or equivalent) with `extra=forbid`; errors name the field; the CLI flags became a profile, not a parallel path. |
| ap1 | Upload streamed to disk in chunks; size limit enforced while streaming; temp file removed on failure. |
| ap2 ★ | Analysis off the event loop (thread or process), with a clear owner; `/health` fast while jobs run. |
| ap3 ★ | A repository object owns all SQL; migrations, not `create_all`; two schemas used for what they mean. |
| wk1 | `serve` lost code; the worker loop is short; claim is one transaction. |
| wk2 ★ | `FOR UPDATE SKIP LOCKED`, and they can explain why plain `FOR UPDATE` and no lock both fail. |
| wk3 ★ | Lease + heartbeat thread; attempts counted at claim; the poison job can't loop forever. Ask what happens if the heartbeat thread dies. |
| wk4 | Signal handler only sets a flag; release only if the job is still theirs; exit 0. |
| rs1 | Batched inserts (executemany), not one statement per row; index matches the query (job_id, t_s). |
| rs2 ★ | One SQL query with window functions; bound parameters, no f-strings into SQL; they can read the plan. |
| rs3 | Cleanup in the claim's transaction (or attempt-tagged rows, or upsert); they can say why they chose it. Episodes keyed by a hash of (video, rule, start) with `ON CONFLICT`, written in the same transaction that marks the job DONE. |
| pf1 | One function owns detector size; profile before/after (Scalene or py-spy) in the PR. |
| pf2 ★ | Work sent to processes as ranges, not frames; `spawn`; a measured speed-up in the PR; memory bounded. |
| pf3 | A dead pool process costs an attempt and replaces the pool; no hang. |
| md1 ★ | The detector is a profile setting behind one interface, not an `if` scattered through the worker; the model's size and limits read from its metadata and config, not hard-coded; BGR→RGB and the box scaling in one place, unit-tested. |
| md2 ★ | Batches of `max_batch_size`; requests in flight bounded by the model's instances; the reader blocks when they're all out (no `Executor.map` over the whole video). Ask them to show the memory with and without the bound. |
| md3 | One retry policy in the client: 503 and connection errors retried with backoff, 4xx and 500 not; a timeout on every call, metadata included; the job's error names the model server. |
| op1 | Context in log fields (`extra`), JSON formatting in one place; no ids in metric labels; job counts and frames from the DB; fps computed in the worker and stored with the job. |
| op2 ★ | NOTIFY in the batch's transaction; LISTEN before the first read; async endpoint with its own connection. |
| op3 ★ | 503 via one exception handler; liveness vs readiness; worker backs off and never records a job FAILED for a DB blip. |
| hd1 | `hmac.compare_digest`; one place that applies auth; the key not in logs or reprs. |

## Things the tester can't check

Ask for these in review: type hints that `basedpyright` (the team's checker) accepts, `ruff` clean, unit tests for pure
logic (segments, events rules, settings), a README section per command, and profiles attached to
the performance PRs.
