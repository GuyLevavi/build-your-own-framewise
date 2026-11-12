"""The tester's model server runs the course's .onnx models with this, one process per instance.

Requests on stdin, answers on stdout, both a JSON line and then raw little-endian float32 bytes:
  in:  {"model": "yolo26n", "shape": [n, 3, h, w]} + the input tensor
  out: {"shape": [...]} + the output tensor, or {"error": "..."} alone
"""

import json
import os
import sys

import numpy as np
import onnxruntime as ort


def main(models_dir: str, threads: int) -> None:
    options = ort.SessionOptions()
    options.intra_op_num_threads = threads
    options.inter_op_num_threads = 1
    sessions = {}
    stdin, stdout = sys.stdin.buffer, sys.stdout.buffer
    stdout.write(b'{"ready": true}\n')
    stdout.flush()
    while line := stdin.readline():
        request = json.loads(line)
        shape = request["shape"]
        data = stdin.read(int(np.prod(shape)) * 4)
        try:
            name = request["model"]
            if name not in sessions:
                path = os.path.join(models_dir, name + ".onnx")
                sessions[name] = ort.InferenceSession(path, options, providers=["CPUExecutionProvider"])
            session = sessions[name]
            x = np.frombuffer(data, dtype="<f4").reshape(shape)
            y = np.ascontiguousarray(session.run(None, {session.get_inputs()[0].name: x})[0], dtype="<f4")
            stdout.write(json.dumps({"shape": list(y.shape)}).encode() + b"\n")
            stdout.write(y.tobytes())
        except Exception as e:  # noqa: BLE001
            stdout.write(json.dumps({"error": f"{type(e).__name__}: {e}"}).encode() + b"\n")
        stdout.flush()


if __name__ == "__main__":
    main(sys.argv[1], int(sys.argv[2]))
