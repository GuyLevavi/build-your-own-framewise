# The models

These are the models the research team hands you. Treat them as black boxes: you run them,
you feed them frames, and you build the service around them. You don't train, change or
look inside them. This file is everything you may rely on.

| File | What |
| --- | --- |
| `yolo26n.pt` | the detector, for PyTorch: run it with [Ultralytics](https://docs.ultralytics.com/modes/predict/) |
| `yolo26n.onnx` | the same detector exported to ONNX, for a model server (Triton) or onnxruntime |
| `yolo26n.labels.txt` | its 80 labels (COCO), one per line: line *n* (from 0) is class id *n* |
| `yolo26n-cls.pt` | the classifier, for PyTorch with Ultralytics |
| `yolo26n-cls.onnx` | the same classifier in ONNX |
| `yolo26n-cls.labels.txt` | its 1000 labels (ImageNet), as above |

Both are Ultralytics YOLO26 nano models (AGPL-3.0, <https://ultralytics.com/license>).

## yolo26n: the detector

Finds objects in a picture: for each, a label, a score from 0 to 1 and a box.

- With Ultralytics: `YOLO("models/yolo26n.pt")(frame)` takes a BGR frame as OpenCV reads it,
  of any size, and returns boxes in that frame's pixels. `imgsz` (default 640) is the size it
  runs at: smaller is faster and finds small objects less well. It must be a multiple of 32.
- A detection counts when its score is at least **0.5**. Lower ones are guesses.
- The ONNX file takes `images`: float32, shape `[batch, 3, H, W]`, RGB, values 0 to 1
  (pixel / 255). H and W are the size you run it at, 640 by default, multiples of 32. To fit
  a frame in, *letterbox* it: scale it by `min(640 / h, 640 / w)` keeping its aspect ratio,
  and centre it on a 640x640 canvas of grey 114, the rest of the canvas.
- It returns `output0`: float32, `[batch, 300, 6]`. Each row is one detection,
  `x1, y1, x2, y2, score, class_id`, corners in the pixels of the 640x640 input; rows are
  sorted by score, and the unused ones have score 0. Undo the letterbox to get the frame's
  pixels: subtract the padding, divide by the scale.
- No non-maximum suppression is needed: the model does it.

## yolo26n-cls: the classifier

Names what a whole picture shows: a probability for each of its 1000 labels, which add up to 1.
The label with the highest probability is its answer, when that probability is at least **0.5**.

- With Ultralytics: `YOLO("models/yolo26n-cls.pt")(frame)[0].probs` (`.top1`, `.top1conf`).
- The ONNX file takes `images`: float32, `[batch, 3, 224, 224]`, RGB, values 0 to 1. To fit a
  frame in, scale it so its *shorter* side is 224, and cut the 224x224 square out of its middle.
- It returns `output0`: float32, `[batch, 1000]`, the probabilities, in the order of
  `yolo26n-cls.labels.txt`.

## Speed and memory

What a call costs depends on the size the model runs at far more than on the frame's size: a
1080p frame and a 640x360 one cost about the same at 640. Importing PyTorch and loading a `.pt`
takes a few hundred MB and a second or two, once per process. PyTorch runs one call on several
threads; several processes each running it on as many threads as there are cores slow each
other down.
