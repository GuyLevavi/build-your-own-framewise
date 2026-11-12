In this stage, you'll find events (a person crossing a car) with a gaps-and-islands query.

Detections are rows now, so questions about them can be SQL. An event here is a stretch of time in which boxes of two labels overlap, allowing short gaps and ignoring stretches that are too short.

### New here?

- **Window function**: a SQL function that looks at neighbouring rows, such as `lag(t_s) OVER (ORDER BY t_s)` (the previous row's time).
- **Gaps and islands**: the classic SQL pattern for grouping consecutive rows into runs (islands) separated by gaps.
- **CTE**: `WITH name AS (SELECT ...)`, a named step inside one query, so a long query reads top to bottom.

### Expected behaviour

- `FRAMEWISE_EVENTS` points at a YAML file of rules, read by `serve`:

  ```yaml
  events:
    crossing:
      labels: [person, car]
      max_gap_s: 0.5
      min_duration_s: 0.15
  ```

  For each rule, a frame "matches" when a box of the first label overlaps a box of the second. Matching frames no more than `max_gap_s` apart form one event, from the first matching frame's `t_s` to the last one's. Events shorter than `min_duration_s` are dropped. Without `FRAMEWISE_EVENTS`, there are no rules.
- `GET /jobs/{id}/events` answers `{"events": [{"rule", "start_s", "end_s"}, ...]}`, sorted by `start_s` then `rule`. An unknown job is 404.
- Events are computed from `results.detections` when asked.

### Tests

The tester writes two rules for person and car (`crossing`, at least 0.15 s, and `long_crossing`, at least 1 s), runs a job on the street clip and checks the events, boundaries within one frame. It then deletes the car's rows between 5.6 and 6.2 s from `results.detections` and asks again: the events must change accordingly. It checks 404 for an unknown job.

### Why your design just broke

You could compute events in Python while the job runs and store them, but then they're a copy that goes stale when rules or rows change, which is what the tester does. Doing it in one query means learning window functions: mark each matching frame that starts a new island (its gap to the previous one is too big), number the islands with a running sum of those marks, and group by that number. The query belongs in the repository, with the rule's values as bound parameters.

### Pointers

- [PostgreSQL: window functions tutorial](https://www.postgresql.org/docs/current/tutorial-window.html) (`lag`, running `sum() OVER (ORDER BY ...)`).
- Search: `gaps and islands sql`, `postgres self join overlapping rectangles`, `common table expressions`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to read your query's plan aloud, and show that every value reaches SQL as a bound parameter, never through an f-string.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Build it in steps, each a CTE: (1) the frames where a box of label A overlaps a box of label B; (2) for each, the gap to the previous matching frame; (3) a flag where that gap is too big (an island starts); (4) a running total of the flags, which numbers the islands; (5) group by that number for start and end, and keep islands long enough.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

The skeleton:

```sql
WITH hits AS (
  SELECT DISTINCT a.t_s FROM results.detections a
  JOIN results.detections b ON a.job_id = b.job_id AND a.frame = b.frame
  WHERE a.job_id = :job AND a.label = :a AND b.label = :b
    AND a.x < b.x + b.w AND b.x < a.x + a.w AND a.y < b.y + b.h AND b.y < a.y + a.h
), marked AS (
  SELECT t_s, CASE WHEN t_s - lag(t_s) OVER (ORDER BY t_s) <= :gap THEN 0 ELSE 1 END AS new
  FROM hits
), numbered AS (
  SELECT t_s, sum(new) OVER (ORDER BY t_s) AS island FROM marked
)
SELECT min(t_s), max(t_s) FROM numbered GROUP BY island
HAVING max(t_s) - min(t_s) >= :min_dur;
```

Run it once per rule, with the rule's values as bound parameters.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

The first row's `lag` is NULL, and `NULL <= gap` is not true, so it correctly starts an island; check your `CASE` does that. Compare times with a small tolerance or round them: 0.1 + 0.2 is not 0.3 in floating point. Never paste a rule's values into the SQL string.

</details>
