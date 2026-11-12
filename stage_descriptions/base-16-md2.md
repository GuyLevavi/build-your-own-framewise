In this stage, you'll send frames to the model server in batches, to reach a frame rate, without reading the whole video ahead.

A model server is fast per frame and slow per request: each call pays for the network, the queue and the launch of the model, however few frames it carries. Batches spread that cost. Frames per second is the number the team judges a video pipeline by, and this stage measures it, together with the worker's memory.

### New here?

- **Batch**: several frames sent in one request, as one tensor of shape `[N, 3, S, S]`.
- **Throughput**: frames finished per second. Latency is how long one request takes; batches raise throughput by sharing each request's fixed cost.
- **`max_batch_size`**: the most frames the model's config allows in one request.

### Expected behaviour

- A job with a remote profile reaches at least **70%** of the frame rate that full batches, sent one at a time, can reach on the model server described below.
- The worker stays under **its idle memory plus 1000 MiB** while it does.
- Labels and stored detections are what they were.

The model's config (`GET /v2/models/yolo26n/config`) says what it accepts: `max_batch_size`, the most frames one request may carry. Here that is 16. Each request costs 300 ms (the network and the server's queue, made slow on purpose) plus the time the model takes on its frames.

### Tests

The tester first times the model on one frame and on a full batch on this machine. That gives the rate of one frame per request (about 3 frames per second), the rate of full batches sent one at a time, and the target, 70% of the latter. It measures your idle worker's memory, then runs the 1080p clip (200 frames) with the remote profile. It measures frames per second from the server's side, from the first request to the last answer, and samples the memory of the worker's whole process group every 50 ms. If the worker goes over three times its budget, the tester stops it. It checks the labels and the row count in `results.detections`.

### Why your design just broke

Your analysis loop has been one frame in, one result out. Now it reads frames into a batch, sends the batch, and yields one result per frame of the answer, in order. The rest of the worker (storing rows, building segments) should not notice: it still consumes one result per frame.

The trap is reading too much at once. "Read every frame, then send them in batches" needs the whole clip in memory: a decoded 1080p frame is 6 MB, and the same frame prepared for the model at 640 is another 5, so 200 frames are over 2 GB. Read one batch, send it, hand out its results, then read the next. Count the copies too: a batch of 16 prepared frames is 79 MB, so fill one array in place rather than stacking a list of them.

### Pointers

- Triton's [dynamic batcher](https://github.com/triton-inference-server/tutorials/tree/main/Conceptual_Guide/Part_2-improving_resource_utilization) explains why servers like batches.
- [`itertools.batched`](https://docs.python.org/3/library/itertools.html#itertools.batched) (Python 3.12) groups any iterator, a generator of frames included, into tuples of N.
- Search: `numpy preallocate array fill`, `python generator batching`, `throughput vs latency`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Group the frames into batches of `max_batch_size`, send each batch as one request, and hand out one result per frame from its answer, in order. Read the next batch only after this one is answered.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

Read `max_batch_size` from `GET /v2/models/yolo26n/config` once. Then:

```python
for batch in itertools.batched(frames(capture, every), max_batch):
    images = np.empty((len(batch), 3, S, S), np.float32)
    for k, (index, frame) in enumerate(batch):
        images[k], fit = prepare(frame, S)
    outputs = client.infer(images)  # one request
    # yield one FrameResult per (index, fit, output), in order
```

</details>

<details>
<summary>Hint 3: the usual traps</summary>

The last batch is usually short: send it anyway. `np.stack` of a list of prepared frames makes a second copy of the batch; filling a preallocated array doesn't. Don't build the list of all frames first.

</details>
