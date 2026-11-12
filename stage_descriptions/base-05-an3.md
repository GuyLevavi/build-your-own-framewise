In this stage, you'll load named analysis profiles from a YAML file, let a profile pick the model, and reject bad ones precisely.

Settings that change between uses belong in config, not code: "people only, every other frame" is a named profile, not a new function, and so is "is that a sports car?", which is a question for the other model in `models/`, the classifier. A config file is also user input, so a mistake in it gets the same clean error as a bad video, and that error names the exact setting that's wrong.

### Expected behaviour

```yaml
# profiles.yaml
profiles:
  people:
    every: 2          # integer, at least 1
    max_gap_s: 0.5    # number, at least 0
    labels: [person]  # non-empty; each one the model has
  parked:
    every: 2
    max_gap_s: 0.5
    model: yolo26n-cls          # yolo26n (the default) or yolo26n-cls
    labels: [sports_car, convertible]
```

```
$ ./your_program.sh analyze street.avi --config profiles.yaml --profile people
{"labels": {"person": [{"start_s": 1.0, "end_s": 8.0}]}}
```

- `--config` defaults to `profiles.yaml`. Without `--profile`, `--every` and `--max-gap` work as before, with the detector and `person`, `car` and `bicycle`.
- A profile's `model` is the model it runs on each sampled frame: `yolo26n`, the detector, or `yolo26n-cls`, the classifier. A label is seen in a frame when the detector finds it with a score of at least 0.5, or when it is the classifier's top label with a probability of at least 0.5 (see `models/README.md`).
- Only the profile's `labels` appear in the output. Each must be one of its model's labels: its file in `models/` lists them.
- If the file is missing or isn't YAML, the error names the file. If a profile is unknown, the error names it.
- If a setting is invalid or unknown (a typo), the error names it by its path, such as `profiles.people.every` or `profiles.people.max_gap`.

### Tests

The tester writes a profiles file and runs two of its profiles on the street clip, and `parked` on a clip of a sports car parked in front of the camera from 2 to 6 s. It checks the labels and how many frames each model ran on. Then it breaks the file eight ways (an unknown profile, `every: 0`, an unknown label, an unknown model, a label the classifier doesn't have, a negative `max_gap_s`, a misspelled key, invalid YAML), and also passes a file that doesn't exist. Each must fail cleanly and name the right thing.

### Why your design just broke

If `--every` and `--max-gap` went straight into your functions as loose arguments, there is now a third source of the same settings. Give them one home, a validated settings object, that both the flags and the YAML produce. Every later stage passes that object around.

And your analysis called the detector by name. Now the profile names the model, and the two answer different questions in different shapes: boxes, or one probability per label. Give them one shape on the way out (a list of labelled boxes, where the classifier's box is the whole frame) and the code after them doesn't need to know which ran.

### Pointers

- Pydantic [Models](https://pydantic.dev/docs/validation/latest/concepts/models/): `extra="forbid"` turns typos into errors, and a `ValidationError`'s `loc` is the path you need.
- A Pydantic [field validator](https://docs.pydantic.dev/latest/concepts/validators/) can check a profile's labels against its model's labels file.
- Search: `yaml.safe_load`, `pydantic ValidationError errors loc`, `numpy argmax`.
