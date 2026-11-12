In this stage, you'll analyze a ten-minute clip within a memory budget.

Real recordings are long. Decoded, ten minutes of even small 320×180 video is about 1 GB of pixels, and an hour of 1080p is hundreds of gigabytes. A service that holds a whole video in memory falls over on the first real file.

### New here?

- **Generator**: a function with `yield`. It hands out one item at a time, when asked, instead of building a whole list first.
- **Resident memory (RSS)**: the RAM a process is using right now. The tester samples it while your program runs.

### Expected behaviour

The same `analyze` as before, but your program (all its processes together) must stay under **400 MiB** of resident memory on a 10-minute, 6000-frame clip.

### Tests

The tester runs `./your_program.sh analyze <long clip> --every 10 --max-gap 2`. It samples your program's memory every 50 ms, checks the labels as before, and checks that the detector ran exactly 600 times.

### Why your design just broke

If you read every frame into a list and then sampled it, the list is the problem. Make frame reading a generator that yields one frame at a time, and let analysis consume it. If you also skip decoding the frames you don't sample (`grab` rather than `read`), you'll be glad of it when the performance stages arrive.

### Pointers

- [How to Use Generators and yield in Python](https://realpython.com/introduction-to-python-generators/) (Real Python).
- Search: `cv2.VideoCapture grab vs read`, `python generator pipeline`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Only one frame needs to be in memory at a time: read it, run the model, keep its labels, drop it.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

`def frames(capture, every): index = 0; while True: ...` that yields `(index, frame)` for every `every`-th frame. For the frames you skip, call `capture.grab()` (moves on without decoding) instead of `capture.read()`. Pass the generator straight into the segment builder.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

A list comprehension over frames, or `list(...)` anywhere on the way, quietly builds the whole list again. So does keeping the frames "for later": keep only `(time, labels)`.

</details>
