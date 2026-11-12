In this stage, you'll reach a frame rate on the model server by batching and overlapping requests, without letting frames pile up in memory.

A model server is fast per frame and slow per request: each call pays for the network, the queue and the launch of the model, however few frames it carries. Batches spread that cost. A server also runs several requests at once, and a client that waits for each answer before sending the next leaves it idle. Frames per second and peak memory are the two numbers the team judges a video pipeline by, and this stage measures both.

### Expected behaviour

- A job with a remote profile reaches at least **1.4 times** the frame rate that full batches sent one at a time can reach on the model server described below.
- The worker stays under **its idle memory plus 400 MiB** while it does.
- Labels and stored detections are what they were.

The model's config says what it accepts: `max_batch_size` frames per request, and `instance_group` with how many requests it runs at once. Here that is 16 frames and 2 requests. Each request costs 300 ms (the network and the server's queue, made slow on purpose) plus the time the model takes on its frames; further requests wait their turn.

### Tests

The tester first times the model on one frame and on a full batch on this machine, which gives the rate of one frame per request (about 3 frames per second), the rate of full batches sent one at a time, and the target, 1.4 times the latter. It measures your idle worker's memory, then runs the 1080p clip (200 frames) with the remote profile. It measures frames per second from the server's side, from the first request to the last answer, and samples the memory of the worker's whole process group every 50 ms. If the worker goes over three times its budget, the tester stops it. It checks the labels and the row count in `results.detections`.

Batches alone make several times the single-frame rate, but each one still waits for the whole round trip while the server's other instance sits idle. Keeping both busy gets close to twice that.

### Why your design just broke

Your analysis loop has been one frame in, one result out. It has to become a pipeline: read frames, group them into batches, keep up to N requests in flight, and yield results in order as the oldest one comes back.

The trap is reading ahead. `ThreadPoolExecutor.map` and "submit everything, then collect" both pull the whole input before the first answer arrives. Decoding is faster than the model, so the frames waiting for it grow until the worker runs out of memory: a decoded 1080p frame is 6 MB, and the same frame prepared for the model at 640 is another 5. The fix is backpressure: when N requests are out, the reader waits. A bounded queue, a semaphore, or a loop that collects the oldest result before submitting the next batch all do it.

### Pointers

- Triton's [dynamic batcher](https://github.com/triton-inference-server/tutorials/tree/main/Conceptual_Guide/Part_2-improving_resource_utilization) explains why servers like batches. Triton can batch requests from many clients itself; sending full batches is still the cheapest path.
- [`concurrent.futures`](https://docs.python.org/3/library/concurrent.futures.html) and [`itertools.batched`](https://docs.python.org/3/library/itertools.html#itertools.batched). Waiting on the network releases the GIL, so threads are fine for this.
- Search: `backpressure bounded queue python`, `Executor.map consumes iterator eagerly`, `Little's law throughput latency concurrency`.
