In this stage, you'll run the detector on a video and report when each label appears.

Your research team handed you a model, `models/yolo26n.onnx`: a detector that finds people, cars and 78 other kinds of object in a picture. Treat it as a black box: you don't train, change or look inside it. `models/README.md` is its contract: what goes in (a float32 tensor of a fixed shape, prepared in a particular way), what comes out, and which scores count. Run it with [onnxruntime](https://onnxruntime.ai/docs/get-started/with-python.html), which is already a dependency.

### New here?

- **Detector**: a model that finds objects in a picture and answers with a label, a score (0 to 1, how sure it is) and a box for each.
- **Tensor**: a NumPy array the model takes or returns. Its *shape* says how it's laid out: `[1, 3, 640, 640]` is a batch of 1 picture, 3 colour channels, 640 rows, 640 columns ("NCHW").
- **BGR / RGB**: the order of the colour channels. OpenCV gives you BGR; the model wants RGB.
- **Letterbox**: shrink the frame to fit a square without stretching it, and pad the rest with grey. The boxes come back in the square's pixels, so you undo the shrink and the padding afterwards.
- **Segment**: a stretch of time in which a label was seen, from its first sighting to its last.

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

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Split the job into three plain functions: frame → tensor (prepare), model output → boxes in the frame's pixels (read), and (time, labels seen) pairs → segments. Only the middle step needs onnxruntime, and the last needs no video at all, so you can test it with a handful of tuples.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

The recipe, for a frame of height `h` and width `w` and S = 640:

1. `scale = min(S / h, S / w)`; resize the frame to `(round(w * scale), round(h * scale))` with `cv2.resize`.
2. Make an S x S canvas filled with 114 and paste the resized frame in its middle; remember the `left` and `top` offsets.
3. `canvas[:, :, ::-1]` (BGR to RGB), `.transpose(2, 0, 1)` (HWC to CHW), `[None]` (add the batch axis), `.astype(np.float32) / 255`.
4. `rows = session.run(None, {"images": tensor})[0][0]`: 300 rows of `x1, y1, x2, y2, score, class_id`.
5. Keep rows with `score >= 0.5`; the frame's box is `x = (x1 - left) / scale`, `w = (x2 - x1) / scale`, and the same for y and h. The label is line `class_id` of `models/yolo26n.labels.txt`.
6. For segments, walk the times in order per label: extend the current segment if the gap to its end is at most `max_gap`, otherwise start a new one.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

Three mistakes find *something*, so they look like they work: feeding BGR, skipping `/ 255`, or forgetting to undo the letterbox (your boxes are then in 640 x 640 pixels). Draw one frame's boxes with `cv2.rectangle` and `cv2.imwrite` and look at it. And create the `InferenceSession` once, not per frame.

</details>
