In this stage, you'll analyze a ten-minute clip within a memory budget.

Real recordings are long. Decoded, ten minutes of even small 320×180 video is about 1 GB of pixels, and an hour of 1080p is hundreds of gigabytes. A service that holds a whole video in memory falls over on the first real file.

### Expected behaviour

The same `analyze` as before, but your program (all its processes together) must stay under **400 MiB** of resident memory on a 10-minute, 6000-frame clip.

### Tests

The tester runs `./your_program.sh analyze <long clip> --every 10 --max-gap 2`. It samples your program's memory every 50 ms, checks the labels as before, and checks that the detector ran exactly 600 times.

### Why your design just broke

If you read every frame into a list and then sampled it, the list is the problem. Make frame reading a generator that yields one frame at a time, and let analysis consume it. If you also skip decoding the frames you don't sample (`grab` rather than `read`), you'll be glad of it when the performance stages arrive.

### Pointers

- [How to Use Generators and yield in Python](https://realpython.com/introduction-to-python-generators/) (Real Python).
- Search: `cv2.VideoCapture grab vs read`, `python generator pipeline`.
