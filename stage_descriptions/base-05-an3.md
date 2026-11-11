In this stage, you'll load named analysis profiles from a YAML file and reject bad ones precisely.

Settings that change between uses belong in config, not code: "people only, every other frame" is a named profile, not a new function. A config file is also user input, so a mistake in it gets the same clean error as a bad video, and that error names the exact setting that's wrong.

### Expected behaviour

```yaml
# profiles.yaml
profiles:
  people:
    every: 2          # integer, at least 1
    max_gap_s: 0.5    # number, at least 0
    labels: [person]  # non-empty; each one of person, car, bicycle
```

```
$ ./your_program.sh analyze street.avi --config profiles.yaml --profile people
{"labels": {"person": [{"start_s": 1.0, "end_s": 8.0}]}}
```

- `--config` defaults to `profiles.yaml`. Without `--profile`, `--every` and `--max-gap` work as before.
- Only the profile's `labels` appear in the output.
- If the file is missing or isn't YAML, the error names the file. If a profile is unknown, the error names it.
- If a setting is invalid or unknown (a typo), the error names it by its path, such as `profiles.people.every` or `profiles.people.max_gap`.

### Tests

The tester writes a profiles file, runs two of its profiles on the street clip, and checks the labels and the detector call counts. Then it breaks the file six ways (an unknown profile, `every: 0`, an unknown label, a negative `max_gap_s`, a misspelled key, invalid YAML), and also passes a file that doesn't exist. Each must fail cleanly and name the right thing.

### Why your design just broke

If `--every` and `--max-gap` went straight into your functions as loose arguments, there is now a third source of the same settings. Give them one home, a validated settings object, that both the flags and the YAML produce. Every later stage passes that object around.

### Pointers

- Pydantic [Models](https://pydantic.dev/docs/validation/latest/concepts/models/): `extra="forbid"` turns typos into errors, and a `ValidationError`'s `loc` is the path you need.
- Search: `yaml.safe_load`, `pydantic ValidationError errors loc`.
