# The models

These are the models the research team hands you. Treat them as black boxes: you run them,
you feed them frames, and you build the service around them. You don't train, change or look
inside them. This file is everything you may rely on.

| File | What |
| --- | --- |
| `yolo26n.onnx` | the detector |
| `yolo26n.labels.txt` | its 80 labels (COCO), one per line: line *n* (from 0) is class id *n* |
| `yolo26n-cls.onnx` | the classifier |
| `yolo26n-cls.labels.txt` | its 1000 labels (ImageNet), as above |

Both are Ultralytics YOLO26 nano models exported to ONNX (AGPL-3.0,
<https://ultralytics.com/license>). Run them with
[onnxruntime](https://onnxruntime.ai/docs/get-started/with-python.html) in your process, or put
them on a model server such as Triton, which runs the same files.

## yolo26n: the detector

Finds objects in a picture: for each, a label, a score from 0 to 1 and a box.

- Input `images`: float32, shape `[batch, 3, H, W]`, RGB (OpenCV reads BGR), values 0 to 1
  (pixel / 255). H and W are the size you run it at: 640 by default, any multiple of 32 works.
  Smaller is faster and finds small objects less well.
- To fit a frame in, *letterbox* it: scale it by `min(S / h, S / w)` (S the input size),
  keeping its aspect ratio, and put it in the middle of an S x S canvas of grey 114.
- Output `output0`: float32, `[batch, 300, 6]`. Each row is one detection,
  `x1, y1, x2, y2, score, class_id`, corners in the pixels of the S x S input. Rows are sorted
  by score; the unused ones have score 0. Undo the letterbox to get the frame's pixels: take
  off the padding, divide by the scale.
- A detection counts when its score is at least **0.5**. Lower ones are guesses.
- No non-maximum suppression is needed: the model does it.

## yolo26n-cls: the classifier

Names what a whole picture shows: a probability for each of its 1000 labels, adding up to 1.
The label with the highest probability is its answer, when that probability is at least **0.5**.

- Input `images`: float32, `[batch, 3, 224, 224]`, RGB, values 0 to 1. To fit a frame in, scale
  it so its *shorter* side is 224, and cut the 224 x 224 square out of its middle.
- Output `output0`: float32, `[batch, 1000]`, the probabilities, in the order of
  `yolo26n-cls.labels.txt`.

## Speed and memory

A call costs what its input costs: a 1080p frame and a 640x360 one cost the same once
letterboxed to 640, and the detector at 320 does a quarter of the work it does at 640. A batch
of N images costs about N single calls, minus the per-call overhead. onnxruntime runs one call
on as many threads as `intra_op_num_threads` (by default, one per core); several processes each
using every core slow each other down.
