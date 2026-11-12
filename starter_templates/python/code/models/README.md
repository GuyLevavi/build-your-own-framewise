# The model

This is the model the research team hands you. Treat it as a black box: you run it, you feed
it frames, and you build the service around it. You don't train, change or look inside it.
This file is everything you may rely on.

| File | What |
| --- | --- |
| `yolo26n.onnx` | the detector |
| `yolo26n.labels.txt` | its 80 labels (COCO), one per line: line *n* (from 0) is class id *n* |

It is Ultralytics' YOLO26 nano model exported to ONNX (AGPL-3.0,
<https://ultralytics.com/license>). Run it with
[onnxruntime](https://onnxruntime.ai/docs/get-started/with-python.html) in your process, or put
it on a model server such as Triton, which runs the same file.

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

## Speed and memory

A call costs what its input costs: a 1080p frame and a 640x360 one cost the same once
letterboxed to 640, and the detector at 320 does a quarter of the work it does at 640. A batch
of N images costs about N single calls, minus the per-call overhead. onnxruntime runs one call
on as many threads as `intra_op_num_threads` (by default, one per core); several processes each
using every core slow each other down.
