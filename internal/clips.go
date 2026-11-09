package internal

import (
	"fmt"
	"image"
	"image/color"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"

	"github.com/codecrafters-io/tester-utils/random"
)

// clip is a synthetic video: a white square on a black (or noisy grey)
// background that moves right by `step` pixels on every frame inside `bursts`
// and stands still otherwise. So the motion frames, and the segments the
// stages expect, are known exactly.
type clip struct {
	name          string
	width, height int
	fps, frames   int
	side, step    int
	bursts        [][2]int // inclusive frame ranges in which the square moves
	noise         int      // amplitude of per-pixel noise around grey 128; 0 means a clean black background
}

var (
	// The stage descriptions quote these clips' numbers; clips_test.go keeps them honest.
	clipSquare      = clip{name: "square", width: 160, height: 120, fps: 10, frames: 40, side: 20, step: 4, bursts: [][2]int{{10, 29}}}
	clipBig         = clip{name: "big", width: 240, height: 180, fps: 15, frames: 45, side: 30, step: 4, bursts: [][2]int{{15, 36}}}
	clipStill       = clip{name: "still", width: 160, height: 120, fps: 10, frames: 30, side: 20, step: 4}
	clipTwoMoves    = clip{name: "two_moves", width: 160, height: 120, fps: 10, frames: 40, side: 20, step: 4, bursts: [][2]int{{5, 12}, {25, 34}}}
	clipPause       = clip{name: "pause", width: 160, height: 120, fps: 10, frames: 40, side: 20, step: 4, bursts: [][2]int{{5, 14}, {18, 27}}}
	clipNoisyStill  = clip{name: "noisy_still", width: 160, height: 120, fps: 10, frames: 30, side: 20, step: 4, noise: 6}
	clipNoisySquare = clip{name: "noisy_square", width: 160, height: 120, fps: 10, frames: 30, side: 20, step: 4, bursts: [][2]int{{8, 21}}, noise: 6}
	clipLong        = clip{name: "long", width: 640, height: 360, fps: 15, frames: 150, side: 60, step: 5, bursts: [][2]int{{30, 119}}}
)

func (c clip) moving(i int) bool {
	for _, b := range c.bursts {
		if i >= b[0] && i <= b[1] {
			return true
		}
	}
	return false
}

// motionFrames are the frames that differ from the previous one.
func (c clip) motionFrames() []int {
	var hits []int
	for i := 1; i < c.frames; i++ {
		if c.moving(i) {
			hits = append(hits, i)
		}
	}
	return hits
}

// squareAt is the top-left corner of the square in frame i.
func (c clip) squareAt(i int) (int, int) {
	moves := 0
	for j := 1; j <= i; j++ {
		if c.moving(j) {
			moves++
		}
	}
	return c.side/2 + moves*c.step, (c.height - c.side) / 2
}

// segments groups motion frames whose gaps are at most maxGapS, as the stages define it.
func (c clip) segments(maxGapS float64) []segment {
	var result []segment
	start, last := -1, -1
	for _, i := range c.motionFrames() {
		if last >= 0 && float64(i-last)/float64(c.fps) > maxGapS+1e-9 {
			result = append(result, segment{float64(start) / float64(c.fps), float64(last) / float64(c.fps)})
			start = -1
		}
		if start < 0 {
			start = i
		}
		last = i
	}
	if start >= 0 {
		result = append(result, segment{float64(start) / float64(c.fps), float64(last) / float64(c.fps)})
	}
	return result
}

func (c clip) render(i int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, c.width, c.height))
	if c.noise > 0 {
		rng := rand.New(rand.NewPCG(uint64(len(c.name)), uint64(i)+1))
		for y := 0; y < c.height; y++ {
			for x := 0; x < c.width; x++ {
				v := uint8(128 + rng.IntN(2*c.noise+1) - c.noise)
				img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
			}
		}
	} else {
		for p := 3; p < len(img.Pix); p += 4 {
			img.Pix[p] = 255
		}
	}
	x0, y0 := c.squareAt(i)
	for y := y0; y < y0+c.side; y++ {
		for x := x0; x < x0+c.side; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	return img
}

var (
	encodedMu sync.Mutex
	encoded   = map[string][]byte{}
)

func (c clip) bytes() []byte {
	encodedMu.Lock()
	defer encodedMu.Unlock()
	if data, ok := encoded[c.name]; ok {
		return data
	}
	data, err := encodeMJPEGAVI(c.frames, c.render, c.fps, 90)
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: encoding %s: %v", c.name, err))
	}
	encoded[c.name] = data
	return data
}

// write puts the clip in dir under a random name, like /tmp/x/square-apple.avi.
func (c clip) write(dir string) string {
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.avi", c.name, random.RandomWord()))
	if err := os.WriteFile(path, c.bytes(), 0o644); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: writing %s: %v", path, err))
	}
	return path
}

func (c clip) duration() float64 { return float64(c.frames) / float64(c.fps) }
