"""Record what the course's model reports on every frame of the tester's scenes.

The tester draws its clips (internal/scenes.go) but can't know what a neural network finds in
them, so this runs the detector in the starter's models/ with onnxruntime on the clips exactly as a
learner's program reads them (OpenCV decoding the tester's AVI), preprocessed as models/README.md
says, and writes internal/golden/<scene>.json.gz:

- "detect": per frame, every person, car and bicycle yolo26n reports at 640 with a score of at
  least 0.1, as [label, score, x, y, w, h] in the video's pixels;
- "detect_at": the same at the other sizes the tester runs the model at on this clip (the model
  server's 512).

The tester compares a learner's boxes with these as internal/compare.go does: each box the learner
reports (score at least 0.5) must be one the reference found with at least ALLOWED, in nearly the
same place, at any of the sizes recorded; and of the boxes the reference found with at least
REQUIRED at the learner's size, 90% must be reported. Detections in between may go either way:
their score moves with how a frame is prepared, and a 1080p frame shrunk to 320 is noisy. This
script checks that the clips leave no more room for doubt than that: the other reasonable ways to
prepare a frame (the letterbox in the corner instead of the middle, another interpolation) never
report a box the reference didn't, miss at most MAX_MISSING of the clear ones, and at 640 agree
on which labels each frame shows. Run it after changing a scene or a model,
with any Python that has onnxruntime and opencv (the course's dev shell does):

    (cd tester && FRAMEWISE_DUMP_SCENES=/tmp/scenes go test -count=1 -run TestDumpScenes ./internal)
    python tester/golden/make_golden.py /tmp/scenes [scene ...]
    make -C tester test
"""

from __future__ import annotations

import gzip
import json
import sys
from pathlib import Path

import cv2
import numpy as np
import onnxruntime as ort

ROOT = Path(__file__).resolve().parents[2]
MODELS = ROOT / "starter_templates/python/code/models"
OUT = ROOT / "tester/internal/golden"
LABELS = ("person", "car", "bicycle")
COUNTS = 0.5  # a detection counts from this score
ALLOWED, REQUIRED = 0.3, 0.7  # see the header; internal/compare.go has the same numbers
MIN_IOU = 0.85  # between the reference and the variants at the same size
MAX_MISSING = 0.05  # of the clear boxes, a variant may miss this many (the tester allows 10%)
SIZES = {"street": (640, 512), "hd_street": (640, 512)}  # the sizes the tester runs at
LABELS_ONLY = {"noisy_street"}  # the tester checks only which labels these show, not boxes


def frames(path: Path):
    capture = cv2.VideoCapture(str(path))
    while True:
        ok, frame = capture.read()
        if not ok:
            break
        yield frame
    capture.release()


def names(model: str) -> list[str]:
    return (MODELS / f"{model}.labels.txt").read_text().splitlines()


def session(model: str) -> ort.InferenceSession:
    return ort.InferenceSession(str(MODELS / f"{model}.onnx"), providers=["CPUExecutionProvider"])


class Detector:
    def __init__(self) -> None:
        self.session = session("yolo26n")
        self.names = names("yolo26n")

    def __call__(self, frame, size=640, centred=True, interpolation=cv2.INTER_LINEAR) -> list[list]:
        h, w = frame.shape[:2]
        r = min(size / h, size / w)
        nw, nh = round(w * r), round(h * r)
        top, left = ((size - nh) // 2, (size - nw) // 2) if centred else (0, 0)
        canvas = np.full((size, size, 3), 114, np.uint8)
        canvas[top : top + nh, left : left + nw] = cv2.resize(frame, (nw, nh), interpolation=interpolation)
        x = canvas[:, :, ::-1].transpose(2, 0, 1)[None].astype(np.float32) / 255
        result = []
        for x1, y1, x2, y2, score, cls in self.session.run(None, {"images": x})[0][0]:
            label = self.names[int(cls)]
            if label in LABELS and score >= 0.1:
                x1, x2 = (x1 - left) / r, (x2 - left) / r
                y1, y2 = (y1 - top) / r, (y2 - top) / r
                result.append([label, round(float(score), 3), round(x1), round(y1), round(x2 - x1), round(y2 - y1)])
        return result



def iou(a: list, b: list) -> float:
    ax, ay, aw, ah = a[2:]
    bx, by, bw, bh = b[2:]
    ix = max(0, min(ax + aw, bx + bw) - max(ax, bx))
    iy = max(0, min(ay + ah, by + bh) - max(ay, by))
    inter = ix * iy
    return inter / (aw * ah + bw * bh - inter)


def labels_at(rows: list[list]) -> set[str]:
    return {r[0] for r in rows if r[1] >= COUNTS}


def compare(rows: dict[int, list[list]], size: int, got: list[list], min_iou: float) -> tuple[list[str], int, int]:
    """What internal/compare.go would make of got, a learner's rows for one frame at size:
    the boxes it can't match, and how many of the boxes it must find (of how many) are missing.

    rows is the reference at every size this frame was recorded at: a box is allowed if the
    reference found it at any of them (a frame shrunk a lot is noisy), and required if the
    reference found it clearly at this size.
    """
    allowed = sorted((r for size_rows in rows.values() for r in size_rows if r[1] >= ALLOWED), key=lambda r: -r[1])
    used = set()
    unexpected = []
    for g in (g for g in got if g[1] >= COUNTS):
        # Of the reference's boxes in the same place, the most certain: the model sometimes
        # reports an object twice, once with a low score.
        best = next(
            (k for k, r in enumerate(allowed) if k not in used and r[0] == g[0] and iou(r, g) >= min_iou),
            None,
        )
        if best is None:
            unexpected.append(f"{g[0]} {g[1]:.2f} isn't one the reference found")
        else:
            used.add(best)
    required = [r for r in rows[size] if r[1] >= REQUIRED]
    matched = [allowed[k] for k in used]
    missing = sum(1 for r in required if not any(m[0] == r[0] and iou(m, r) >= min_iou for m in matched))
    return unexpected, missing, len(required)


def main(scenes_dir: str, only: set[str]) -> int:
    detect = Detector()
    problems: list[str] = []
    for path in sorted(Path(scenes_dir).glob("*.avi")):
        scene = path.stem
        if only and scene not in only:
            continue
        sizes = SIZES.get(scene, (640,))
        detections = {size: [] for size in sizes}
        count = 0
        missing = {(size, what): [0, 0] for size in sizes for what in ("corner", "INTER_AREA")}
        for i, frame in enumerate(frames(path)):
            count += 1
            rows = {size: detect(frame, size) for size in sizes}
            for size in sizes:
                detections[size].append(rows[size])
            if float(frame.std()) < 1:  # a poison frame: the tester expects nothing there
                continue
            for size in sizes:
                for what, variant in (
                    ("corner", detect(frame, size, centred=False)),
                    ("INTER_AREA", detect(frame, size, interpolation=cv2.INTER_AREA)),
                ):
                    unexpected, n, of = compare(rows, size, variant, MIN_IOU)
                    if scene not in LABELS_ONLY:
                        problems += [f"{scene} frame {i}: at {size}, {what}: {u}" for u in unexpected]
                    missing[size, what][0] += n
                    missing[size, what][1] += of
                    if size == 640 and labels_at(rows[size]) != labels_at(variant):
                        problems.append(f"{scene} frame {i}: {what} finds {sorted(labels_at(variant))}, the reference {sorted(labels_at(rows[size]))}")
        for (size, what), (n, of) in missing.items():
            print(f"{scene} at {size}, {what}: {n} of {of} clear boxes missing", file=sys.stderr)
            if n > MAX_MISSING * of:
                problems.append(f"{scene} at {size}, {what}: {n} of {of} clear boxes missing, over {MAX_MISSING:.0%}")
        data = {
            "scene": scene,
            "frames": count,
            "detect": detections[640],
            "detect_at": {str(size): rows for size, rows in detections.items() if size != 640},
        }
        OUT.mkdir(parents=True, exist_ok=True)
        with gzip.GzipFile(OUT / f"{scene}.json.gz", "wb", mtime=0) as f:
            f.write(json.dumps(data, separators=(",", ":")).encode())
        print(f"{scene}: {len(classes)} frames", file=sys.stderr)
    for p in problems:
        print(p, file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1], set(sys.argv[2:])))
