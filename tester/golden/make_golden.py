"""Record what the course's model reports on every frame of the tester's scenes.

The tester draws its clips (internal/scenes.go) but can't know what a neural network finds in
them, so this runs the course's reference pipeline on the clips exactly as a learner's program
reads them (OpenCV decoding the tester's AVI) and writes internal/golden/<scene>.json.gz:

- "detect": per frame, every person, car and bicycle YOLO26n (the .pt, with Ultralytics, at its
  default 640) reports with a score of at least 0.1, as [label, score, x, y, w, h] in the video's
  pixels;
- "classify": per frame, YOLO26n-cls's top class and its probability (only the parked clip is
  ever classified, so only its frames are checked).

It then checks that the clips leave no room for doubt: every detection that counts (score at least
0.5, as the stage texts say) is well above it, nothing else comes close, and the other ways a learner
may run the model (the .onnx with a letterbox of their own at 640, Ultralytics at imgsz 320 for the
1080p clip) find the same objects in nearly the same boxes. Run it after changing a scene or a
model, from the course's dev shell or any Python with ultralytics and onnxruntime:

    (cd tester && FRAMEWISE_DUMP_SCENES=/tmp/scenes go test -count=1 -run TestDumpScenes ./internal)
    python tester/golden/make_golden.py /tmp/scenes [scene ...]
    make -C tester test
"""

from __future__ import annotations

import ast
import gzip
import json
import sys
from pathlib import Path

import cv2
import numpy as np
import onnxruntime as ort
from ultralytics import YOLO

ROOT = Path(__file__).resolve().parents[2]
MODELS = ROOT / "starter_templates/python/code/models"
OUT = ROOT / "tester/internal/golden"
LABELS = ("person", "car", "bicycle")
COUNTS = 0.5  # a detection counts from this score
MARGIN = (0.3, 0.7)  # no score of a counted label may fall in here
MIN_IOU = 0.85  # between the reference and the other pipelines


def frames(path: Path):
    capture = cv2.VideoCapture(str(path))
    while True:
        ok, frame = capture.read()
        if not ok:
            break
        yield frame
    capture.release()


def letterbox(frame: np.ndarray, size: int = 640) -> tuple[np.ndarray, float, int, int]:
    h, w = frame.shape[:2]
    r = min(size / h, size / w)
    nw, nh = round(w * r), round(h * r)
    resized = cv2.resize(frame, (nw, nh), interpolation=cv2.INTER_LINEAR)
    top, left = (size - nh) // 2, (size - nw) // 2
    out = np.full((size, size, 3), 114, np.uint8)
    out[top : top + nh, left : left + nw] = resized
    return out, r, left, top


def center_crop(frame: np.ndarray, size: int = 224) -> np.ndarray:
    h, w = frame.shape[:2]
    r = size / min(h, w)
    resized = cv2.resize(frame, (round(w * r), round(h * r)), interpolation=cv2.INTER_LINEAR)
    h, w = resized.shape[:2]
    top, left = (h - size) // 2, (w - size) // 2
    return resized[top : top + size, left : left + size]


class OnnxClassifier:
    def __init__(self, path: Path) -> None:
        self.session = ort.InferenceSession(str(path), providers=["CPUExecutionProvider"])
        self.names = ast.literal_eval(self.session.get_modelmeta().custom_metadata_map["names"])

    def __call__(self, frame: np.ndarray) -> list:
        x = center_crop(frame)[:, :, ::-1].transpose(2, 0, 1)[None].astype(np.float32) / 255
        probs = self.session.run(None, {"images": x})[0][0]
        return [self.names[int(probs.argmax())], float(probs.max())]


class OnnxDetector:
    def __init__(self, path: Path) -> None:
        self.session = ort.InferenceSession(str(path), providers=["CPUExecutionProvider"])
        self.names = ast.literal_eval(self.session.get_modelmeta().custom_metadata_map["names"])

    def __call__(self, frame: np.ndarray) -> list[list]:
        image, r, left, top = letterbox(frame)
        x = image[:, :, ::-1].transpose(2, 0, 1)[None].astype(np.float32) / 255
        rows = self.session.run(None, {"images": x})[0][0]
        result = []
        for x1, y1, x2, y2, score, cls in rows:
            label = self.names[int(cls)]
            if label in LABELS and score >= 0.1:
                x1, x2 = (x1 - left) / r, (x2 - left) / r
                y1, y2 = (y1 - top) / r, (y2 - top) / r
                result.append([label, float(score), round(x1), round(y1), round(x2 - x1), round(y2 - y1)])
        return result


def ultralytics_boxes(result) -> list[list]:
    rows = []
    for (x1, y1, x2, y2), score, cls in zip(
        result.boxes.xyxy.tolist(), result.boxes.conf.tolist(), result.boxes.cls.tolist()
    ):
        label = result.names[int(cls)]
        if label in LABELS:
            rows.append([label, round(score, 3), round(x1), round(y1), round(x2 - x1), round(y2 - y1)])
    return rows


def iou(a: list, b: list) -> float:
    ax, ay, aw, ah = a[2:]
    bx, by, bw, bh = b[2:]
    ix = max(0, min(ax + aw, bx + bw) - max(ax, bx))
    iy = max(0, min(ay + ah, by + bh) - max(ay, by))
    inter = ix * iy
    return inter / (aw * ah + bw * bh - inter)


def counted(rows: list[list]) -> dict[str, list]:
    return {r[0]: r for r in rows if r[1] >= COUNTS}


def check(scene: str, i: int, reference: list[list], other: list[list], what: str, min_iou: float) -> list[str]:
    problems = []
    for r in reference:
        if MARGIN[0] <= r[1] < MARGIN[1]:
            problems.append(f"{scene} frame {i}: {r[0]} scores {r[1]:.2f}, too close to {COUNTS}")
    a, b = counted(reference), counted(other)
    if a.keys() != b.keys():
        problems.append(f"{scene} frame {i}: the reference finds {sorted(a)}, {what} {sorted(b)}")
    for label in a.keys() & b.keys():
        if (v := iou(a[label], b[label])) < min_iou:
            problems.append(f"{scene} frame {i}: {label} IoU {v:.2f} between the reference and {what}")
    return problems


def main(scenes_dir: str) -> int:
    detector = YOLO(str(MODELS / "yolo26n.pt"))
    classifier = YOLO(str(MODELS / "yolo26n-cls.pt"))
    onnx = OnnxDetector(MODELS / "yolo26n.onnx")
    onnx_classifier = OnnxClassifier(MODELS / "yolo26n-cls.onnx")
    problems: list[str] = []
    only = set(sys.argv[2:])
    for path in sorted(Path(scenes_dir).glob("*.avi")):
        scene = path.stem
        if only and scene not in only:
            continue
        detect, classify = [], []
        for i, frame in enumerate(frames(path)):
            reference = ultralytics_boxes(detector(frame, conf=0.1, verbose=False)[0])
            detect.append(reference)
            probs = classifier(frame, verbose=False)[0].probs
            classify.append([classifier.names[probs.top1], round(float(probs.top1conf), 3)])
            if float(frame.std()) < 1:  # a poison frame: the tester expects nothing there
                continue
            problems += check(scene, i, reference, onnx(frame), "the .onnx at 640", MIN_IOU)
            if frame.shape[1] > 1280:
                small = ultralytics_boxes(detector(frame, conf=0.1, imgsz=320, verbose=False)[0])
                problems += check(scene, i, reference, small, "Ultralytics at imgsz 320", 0.75)
            # Only the parked clip is ever classified.
            top = classify[-1]
            if scene == "parked":
                if MARGIN[0] <= top[1] < MARGIN[1]:
                    problems.append(f"{scene} frame {i}: the classifier's top class {top[0]} at {top[1]:.2f}")
                other = onnx_classifier(frame)
                if (top[1] >= COUNTS) != (other[1] >= COUNTS) or (top[1] >= COUNTS and top[0] != other[0]):
                    problems.append(f"{scene} frame {i}: the reference classifies {top}, the .onnx with a centre crop {other}")
        data = {"scene": scene, "frames": len(detect), "detect": detect, "classify": classify}
        OUT.mkdir(parents=True, exist_ok=True)
        with gzip.GzipFile(OUT / f"{scene}.json.gz", "wb", mtime=0) as f:
            f.write(json.dumps(data, separators=(",", ":")).encode())
        print(f"{scene}: {len(detect)} frames", file=sys.stderr)
    for p in problems:
        print(p, file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
