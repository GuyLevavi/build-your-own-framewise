"""fwdetect: the object detector the Framewise course ships as a black box.

Treat it like a model the researchers handed you: you call it, you feed it
frames, and you make the service around it fast and robust. You don't change
it. What you may rely on is written here; anything else is an implementation
detail.

    from fwdetect import detect
    boxes = detect(frame)  # frame: H x W x 3 uint8, BGR, as cv2 gives it

Contract:

- ``detect(frame) -> list[Box]``: one call per frame, at most one box per
  label, in the coordinates of the frame you passed.
- Labels are ``person``, ``car`` and ``bicycle``.
- Frames must be at least ``MIN_WIDTH`` (320) pixels wide; smaller frames
  raise ``ValueError``. Accuracy is the same at any width from there up.
- Cost grows with the number of pixels you pass, and most of it holds the
  GIL, like many Python-wrapped models do.
- Known bug: a uniform mid-grey frame (a camera that dropped its feed) crashes
  the process with SIGABRT. It cannot be caught. Plan for it.
- Debugging: with ``FWDETECT_TRACE=<path>`` every call appends one JSON line
  ``{"pid", "t", "w", "h", "boxes"}`` to that file.
- ``python -m fwdetect bench`` measures single-core frames per second on this
  machine at 640x360 and prints it as JSON.
"""

from fwdetect._core import LABELS, MIN_WIDTH, Box, detect

__all__ = ["LABELS", "MIN_WIDTH", "Box", "detect"]
__version__ = "0.1.0"
