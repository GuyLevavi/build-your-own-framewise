"""The Framewise tester's hook in your processes. You don't need to read this.

The tester puts this directory first on PYTHONPATH, so Python imports this file when it starts,
in your program and in every process it starts. It changes nothing your code can see, except
the two things below, both by wrapping the two ways the course's models run:
onnxruntime.InferenceSession.run, and Ultralytics' AutoBackend.forward for a .pt model.

- With FRAMEWISE_TRACE=<path>, every model call appends one JSON line to that file:
  {"pid", "t", "model", "n", "w", "h"}: the process, the time, "yolo26n" or "yolo26n-cls", the
  batch size and the input tensor's width and height. That is how the tester counts the frames
  you run the model on, and in which processes.
- It simulates a known bug of the inference runtime: if an image in the batch is uniform
  mid-grey (a camera that dropped its feed), the process aborts with SIGABRT, as a crash in
  native code does. Nothing in Python can catch it.

After installing the wrappers it runs any other sitecustomize on the path, as Python would have.
"""

import importlib.abc
import importlib.machinery
import json
import os
import sys
import time

_HERE = os.path.dirname(os.path.abspath(__file__))
_TRACE = os.environ.get("FRAMEWISE_TRACE")
_TARGETS = ("onnxruntime.capi.onnxruntime_inference_collection", "ultralytics.nn.autobackend")


def _model_name(names_count, task):
    if task == "classify" or names_count == 1000:
        return "yolo26n-cls"
    return "yolo26n"


def _is_dropped_feed(images):
    """images: N x 3 x H x W, floats in 0..1 (or 0..255). Which of them are uniform grey 128."""
    import numpy as np

    x = np.asarray(images, dtype=np.float32)[:, :, ::8, ::8]
    if x.size and float(x.max()) > 1.5:
        x = x / 255.0
    result = []
    for image in x:
        near = np.abs(image - 128 / 255) < 8 / 255  # the dropped feed
        grey = (image > 100 / 255) & (image < 140 / 255)  # and the letterbox's 114 around it
        result.append(bool(near.mean() > 0.4 and grey.mean() > 0.98))
    return result


def _observe(model, images):
    shape = getattr(images, "shape", None)
    if shape is None or len(shape) != 4:
        return
    if any(_is_dropped_feed(images)):
        os.abort()
    if not _TRACE:
        return
    line = json.dumps(
        {"pid": os.getpid(), "t": time.time(), "model": model, "n": int(shape[0]), "w": int(shape[3]), "h": int(shape[2])}
    )
    # One O_APPEND write per line, so lines from many processes never interleave.
    fd = os.open(_TRACE, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o644)
    try:
        os.write(fd, (line + "\n").encode())
    finally:
        os.close(fd)


def _patch_onnxruntime(module):
    cls = module.InferenceSession
    original = cls.run

    def run(self, output_names, input_feed, *args, **kwargs):
        try:
            name = self.__dict__.get("_framewise_model")
            if name is None:
                meta = self.get_modelmeta().custom_metadata_map
                name = _model_name(meta.get("names", "").count(":"), meta.get("task"))
                self.__dict__["_framewise_model"] = name
            for value in (input_feed or {}).values():
                _observe(name, value)
                break
        except Exception:
            pass
        return original(self, output_names, input_feed, *args, **kwargs)

    cls.run = run


def _patch_ultralytics(module):
    cls = module.AutoBackend
    original = cls.forward

    def forward(self, im, *args, **kwargs):
        try:
            fmt = getattr(self, "format", None) or ("pt" if getattr(self, "pt", False) else None)
            if fmt in ("pt", "torchscript") and sys._getframe(1).f_code.co_name != "warmup":
                names = getattr(self, "names", None) or {}
                task = getattr(getattr(self, "model", None), "task", None)
                images = im.detach().float().cpu().numpy() if hasattr(im, "detach") else im
                _observe(_model_name(len(names), task), images)
        except Exception:
            pass
        return original(self, im, *args, **kwargs)

    cls.forward = forward


_PATCHES = {_TARGETS[0]: _patch_onnxruntime, _TARGETS[1]: _patch_ultralytics}


class _Loader(importlib.abc.Loader):
    def __init__(self, loader, patch):
        self.loader, self.patch = loader, patch

    def create_module(self, spec):
        return self.loader.create_module(spec)

    def exec_module(self, module):
        self.loader.exec_module(module)
        try:
            self.patch(module)
        except Exception:
            pass


class _Finder(importlib.abc.MetaPathFinder):
    def find_spec(self, name, path, target=None):
        if name not in _PATCHES:
            return None
        for finder in sys.meta_path:
            if finder is self or not hasattr(finder, "find_spec"):
                continue
            spec = finder.find_spec(name, path, target)
            if spec is not None and spec.loader is not None:
                spec.loader = _Loader(spec.loader, _PATCHES[name])
                return spec
        return None


sys.meta_path.insert(0, _Finder())


def _chain():
    paths = [p for p in sys.path if os.path.abspath(p or ".") != _HERE]
    spec = importlib.machinery.PathFinder.find_spec("sitecustomize", paths)
    if spec is not None and spec.loader is not None:
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)


try:
    import importlib.util

    _chain()
except Exception:
    pass
