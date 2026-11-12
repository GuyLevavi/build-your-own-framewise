In this stage, you'll load named analysis profiles from a YAML file and reject bad ones precisely.

Settings that change between uses belong in config, not code: "people only, every other frame" is a named profile, not a new function. A config file is also user input, so a mistake in it gets the same clean error as a bad video, and that error names the exact setting that's wrong.

### New here?

- **YAML**: a text format for configuration, indented like Python. `yaml.safe_load` turns it into dicts and lists.
- **Pydantic model**: a class that declares fields with types and limits, and checks data against them when you build one.
- **Profile**: a named set of analysis settings (which frames, which labels, how long a gap may be).

### Expected behaviour

```yaml
# profiles.yaml
profiles:
  people:
    every: 2          # integer, at least 1
    max_gap_s: 0.5    # number, at least 0
    labels: [person]  # non-empty; each one the model has
  traffic:
    every: 5
    max_gap_s: 1.0
    labels: [person, car]
```

```
$ ./your_program.sh analyze street.avi --config profiles.yaml --profile people
{"labels": {"person": [{"start_s": 1.0, "end_s": 8.0}]}}
```

- `--config` defaults to `profiles.yaml`. Without `--profile`, `--every` and `--max-gap` work as before, with the detector and `person`, `car` and `bicycle`.
- Only the profile's `labels` appear in the output. Each must be one of the detector's labels: `models/yolo26n.labels.txt` lists them.
- If the file is missing or isn't YAML, the error names the file. If a profile is unknown, the error names it.
- If a setting is invalid or unknown (a typo), the error names it by its path, such as `profiles.people.every` or `profiles.people.max_gap`.

### Tests

The tester writes a profiles file and runs two of its profiles on the street clip. It checks the labels and how many frames the detector ran on. Then it breaks the file seven ways (an unknown profile, `every: 0`, `every: often`, an unknown label, a negative `max_gap_s`, a misspelled key, invalid YAML), and also passes a file that doesn't exist. Each must fail cleanly and name the right thing.

### Why your design just broke

If `--every` and `--max-gap` went straight into your functions as loose arguments, there is now a third source of the same settings. Give them one home, a validated settings object, that both the flags and the YAML produce. Every later stage passes that object around, and later stages add settings to it.

### Pointers

- Pydantic [Models](https://pydantic.dev/docs/validation/latest/concepts/models/): `extra="forbid"` turns typos into errors, and a `ValidationError`'s `loc` is the path you need.
- A Pydantic [field validator](https://docs.pydantic.dev/latest/concepts/validators/) can check a profile's labels against the detector's labels file.
- Search: `yaml.safe_load`, `pydantic ValidationError errors loc`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to show how a misspelled key is rejected, where the list of valid labels comes from, and how `--every` and `--max-gap` without `--profile` reuse the profile path rather than a parallel one.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

One validated `Profile` object that both the command-line flags and the YAML file produce. Everything after that takes a `Profile`, never loose arguments.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

`class Profile(BaseModel)` with `model_config = ConfigDict(extra="forbid", frozen=True)`, `every: int = Field(1, ge=1)`, `max_gap_s: float = Field(0.5, ge=0)`, `labels: tuple[str, ...]`; and `class ProfilesFile(BaseModel): profiles: dict[str, Profile]`. Catch `ValidationError`, take `e.errors()[0]["loc"]` (such as `('profiles', 'people', 'every')`) and join it with dots into your error message.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`extra="forbid"` is what turns a typo such as `max_gap` into an error instead of a silently ignored setting. A file that is missing or isn't YAML raises before Pydantic sees anything: catch `FileNotFoundError` and `yaml.YAMLError` and turn them into your error type too.

</details>
