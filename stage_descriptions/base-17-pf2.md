In this stage, you'll split one job across processes, and find out what the parent process costs you.

More workers make more jobs finish per minute, but one long video is still analyzed on one core. Threads won't help: the detector holds the GIL for most of its work (`fwdetect` says so, and you can measure it). Processes will, if you feed them well.

### Expected behaviour

- `FRAMEWISE_DETECT_PROCESSES` (default 1) sets how many processes a worker runs the detector in.
- With N processes (up to 4) a job runs at least 1 + 0.4 × (N − 1) times as many frames per second as with 1: 2.2× for 4.
- The worker and its processes together stay under 300 + 120 × N MiB.
- Results are exactly what one process produces, stored in order.

### Tests

This stage needs at least 2 CPUs; on fewer it fails and says so. The tester runs a job on the 1080p clip with `FRAMEWISE_DETECT_PROCESSES=1`, then with N = min(CPUs, 4), sampling the memory of the worker's whole process group. It checks the speed-up, that the trace shows N different processes calling the detector, the memory limit, the row count in `results.detections`, and the detections between 8 and 10 s.

### Why your design just broke

The obvious version (the parent decodes each frame and sends it to a pool) runs into a ceiling: decoding and resizing are serial in the parent, and every frame is copied between processes. Profile the parent. The fix is to send work, not data: give each process a range of frames to decode itself. Then the loop that consumed frames one at a time has to consume chunks of results in order, and the "should I stop?" check moves between chunks. Use `spawn`: forking a process that has threads and open database connections is asking for trouble.

### Pointers

- [`multiprocessing`](https://docs.python.org/3/library/multiprocessing.html) (Pool, contexts and start methods) and [`concurrent.futures`](https://docs.python.org/3/library/concurrent.futures.html).
- Amdahl's law; David Beazley's talk "Understanding the Python GIL".
- Search: `cv2 CAP_PROP_POS_FRAMES seek`, `multiprocessing spawn vs fork threads`, `pickle cost numpy array`.
