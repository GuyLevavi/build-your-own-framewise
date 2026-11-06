In this stage, you'll load named detector settings from YAML.

Tuning thresholds should not need a code change. A new variant is a new YAML entry, never a new class.

### Expected behaviour

```yaml
# detectors.yaml
max_gap_s: 0.5
detectors:
  sensitive:
    threshold: 15
    min_area: 20
  coarse:
    threshold: 60
    min_area: 400
```

```
$ ./your_program.sh analyze VIDEO --config detectors.yaml --detector coarse
```

- `--config PATH` loads the file. `max_gap_s` is the gap rule from stage 8; `threshold` and `min_area` are the rule from stage 9, per detector.
- `--detector NAME` picks one entry. It defaults to `default`.
- Without `--config`, there is one built-in detector, `default`, with `threshold: 25`, `min_area: 20`, and `max_gap_s` is 0.5. Earlier stages keep passing.

### Tests

The tester writes configs with random detector names and checks that:

- a detector with `min_area: 1000` finds no motion in the moving-square clip, and one with `min_area: 50` finds it;
- `max_gap_s: 0.25` splits the pause clip from stage 8 into two segments.

### Notes

- `yaml.safe_load` (PyYAML) is installed. Load the file into a small dataclass rather than passing dicts around.
