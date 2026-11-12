In this stage, you'll run the detector on a video and report when each label appears.

Your research team handed you two models, in `models/`: `yolo26n.onnx`, a detector, and `yolo26n-cls.onnx`, a classifier (you'll need it in a later stage). Treat them as black boxes: you don't train, change or look inside them. `models/README.md` is their contract: what goes in (a float32 tensor of a fixed shape, prepared in a particular way), what comes out, and which scores count. Run the detector with [onnxruntime](https://onnxruntime.ai/docs/get-started/with-python.html), which is already a dependency.

### Expected behaviour

```
$ ./your_program.sh analyze /tmp/street-apple.avi --every 5 --max-gap 0.5
{"labels": {"car": [{"start_s": 4.0, "end_s": 11.0}], "person": [{"start_s": 1.0, "end_s": 8.0}]}}
```

- Run the detector on every `--every`-th frame (default 1): frames 0, N, 2N, and so on. Run it exactly once per sampled frame, and never on the others.
- Prepare each frame as the README says: letterbox it to 640 x 640, RGB, values 0 to 1. A label is seen in a frame when the detector finds it with a score of at least 0.5. Report `person`, `car` and `bicycle`.
- A frame's time is its index divided by the fps.
- For each label seen, report segments from the first sighting to the last. Start a new segment when the gap between two sightings is longer than `--max-gap` seconds (default 0.5).
- Leave out labels that were never seen.

### Tests

The tester analyzes a 12-second street clip (a person walks right, a car drives left) with the defaults, with `--every 5`, and a noisy version with `--every 2 --max-gap 0.3`. Each segment boundary may be off by one frame. The tester watches onnxruntime from inside your process: it records every image each model is run on, so it also checks that the detector ran exactly once per sampled frame. The objects in the clips are clear-cut: other reasonable ways to letterbox or resize find the same ones, but feeding BGR, skipping the `/ 255` or forgetting to undo the letterbox won't.

### Pointers

- Keep turning sightings into segments separate from reading the video: it's a plain function of `(time, labels)` pairs that you can test without a video.
- Read `models/README.md` before writing any code, and check your preprocessing on one frame: draw the boxes back onto it with `cv2.rectangle` and look.
- Create the `InferenceSession` once and reuse it: loading a model costs far more than running it on a frame.
- Search: `onnxruntime InferenceSession run`, `numpy transpose`, `python dataclass`.
