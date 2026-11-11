In this stage, you'll find events (a person crossing a car) with a gaps-and-islands query.

Detections are rows now, so questions about them can be SQL. An event here is a stretch of time in which boxes of two labels overlap, allowing short gaps and ignoring stretches that are too short.

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
