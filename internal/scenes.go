package internal

import (
	"fmt"
	"image"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"

	"github.com/codecrafters-io/tester-utils/random"
)

// A scene is a synthetic clip for the shipped detector, fwdetect: labeled
// rectangles that move along straight lines on a black (or dark, noisy)
// background. Each label has its own colour channel, and rectangles are drawn
// additively, so where a person and a car overlap the pixels are magenta and
// fwdetect still finds both whole boxes. So the boxes in every frame, and the
// presence and overlap segments built from them, are known exactly.
//
// Rules the stage texts rely on (scenes_test.go checks them):
//   - at most one object of each label is visible in any frame;
//   - every box stays inside the frame;
//   - a poison frame is uniform mid-grey, which fwdetect treats as a dropped
//     feed and aborts on (SIGABRT), the way a native library bug kills a worker.
type scene struct {
	name          string
	width, height int
	fps, frames   int
	objects       []object
	noise         int   // amplitude of per-pixel noise around dark grey 40; 0 means black
	poison        []int // frames rendered as uniform grey 128
}

type object struct {
	label string
	w, h  int
	path  []keyframe // visible from path[0].frame to the last keyframe's frame, inclusive
}

// keyframe puts the object's top-left corner at (x, y) in frame `frame`;
// between keyframes it moves linearly.
type keyframe struct{ frame, x, y int }

type box struct {
	Label      string
	X, Y, W, H int
}

func (b box) intersects(o box) bool {
	return b.X < o.X+o.W && o.X < b.X+b.W && b.Y < o.Y+o.H && o.Y < b.Y+b.H
}

// labelChannels is the colour of each label: one RGB channel each.
var labelChannels = map[string][3]uint8{
	"person":  {255, 0, 0},
	"car":     {0, 0, 255},
	"bicycle": {0, 255, 0},
}

const (
	poisonGrey     = 128
	noiseBaseLevel = 40
)

func passes(label string, w, h, startFrame, endFrame, x0, x1, y int) object {
	return object{label: label, w: w, h: h, path: []keyframe{{startFrame, x0, y}, {endFrame, x1, y}}}
}

var (
	// sceneStreet: a person walks right while a car drives left; they cross.
	sceneStreet = scene{name: "street", width: 640, height: 360, fps: 10, frames: 120, objects: []object{
		passes("person", 40, 100, 10, 80, 20, 560, 200),
		passes("car", 160, 80, 40, 110, 470, 10, 220),
	}}
	// sceneNoisyStreet is sceneStreet on a noisy background.
	sceneNoisyStreet = func() scene { s := sceneStreet; s.name = "noisy_street"; s.noise = 8; return s }()
	// sceneLong: ten minutes at 320x180; a person crosses every minute and a car every two.
	sceneLong = func() scene {
		s := scene{name: "long_street", width: 320, height: 180, fps: 10, frames: 6000}
		for k := range 10 {
			s.objects = append(s.objects, passes("person", 20, 50, k*600+100, k*600+200, 10, 290, 60))
		}
		for k := range 5 {
			s.objects = append(s.objects, passes("car", 60, 30, k*1200+400, k*1200+500, 250, 10, 120))
		}
		return s
	}()
	// sceneHD: twenty seconds of 1080p at 25 fps, for the performance stages.
	sceneHD = scene{name: "hd_street", width: 1920, height: 1080, fps: 25, frames: 500, objects: []object{
		passes("person", 120, 300, 25, 475, 100, 1700, 600),
		passes("car", 480, 240, 100, 450, 1400, 100, 700),
	}}
	// scenePoison: a person walks across, and frame 30 is a dropped feed that kills fwdetect.
	scenePoison = scene{name: "poison", width: 640, height: 360, fps: 10, frames: 60, poison: []int{30}, objects: []object{
		passes("person", 40, 100, 0, 59, 20, 580, 130),
	}}

	allScenes = []scene{sceneStreet, sceneNoisyStreet, sceneLong, sceneHD, scenePoison}
)

// boxAt is the object's box in frame i, if it is visible there.
func (o object) boxAt(i int) (box, bool) {
	first, last := o.path[0], o.path[len(o.path)-1]
	if i < first.frame || i > last.frame {
		return box{}, false
	}
	for k := 0; k+1 < len(o.path); k++ {
		a, b := o.path[k], o.path[k+1]
		if i >= a.frame && i <= b.frame {
			span := b.frame - a.frame
			if span == 0 {
				return box{o.label, a.x, a.y, o.w, o.h}, true
			}
			t := i - a.frame
			return box{o.label, a.x + (b.x-a.x)*t/span, a.y + (b.y-a.y)*t/span, o.w, o.h}, true
		}
	}
	return box{o.label, first.x, first.y, o.w, o.h}, true
}

func (s scene) isPoison(i int) bool {
	for _, p := range s.poison {
		if p == i {
			return true
		}
	}
	return false
}

// boxesAt lists the boxes fwdetect should report for frame i (none on a poison frame).
func (s scene) boxesAt(i int) []box {
	if s.isPoison(i) {
		return nil
	}
	var boxes []box
	for _, o := range s.objects {
		if b, ok := o.boxAt(i); ok {
			boxes = append(boxes, b)
		}
	}
	return boxes
}

// presenceFrames are the frames, out of those sampled every `every` frames
// starting at 0, in which label is visible.
func (s scene) presenceFrames(label string, every int) []int {
	var hits []int
	for i := 0; i < s.frames; i += every {
		for _, b := range s.boxesAt(i) {
			if b.Label == label {
				hits = append(hits, i)
				break
			}
		}
	}
	return hits
}

// overlapFrames are the sampled frames in which a box of label a intersects a box of label b.
func (s scene) overlapFrames(a, b string, every int) []int {
	var hits []int
	for i := 0; i < s.frames; i += every {
		boxes := s.boxesAt(i)
		found := false
		for _, x := range boxes {
			for _, y := range boxes {
				if x.Label == a && y.Label == b && x.intersects(y) {
					found = true
				}
			}
		}
		if found {
			hits = append(hits, i)
		}
	}
	return hits
}

// groupFrames turns sorted frame numbers into segments, starting a new one
// whenever the gap between neighbours is longer than maxGapS seconds.
func groupFrames(frames []int, fps int, maxGapS float64) []segment {
	var result []segment
	start, last := -1, -1
	for _, i := range frames {
		if last >= 0 && float64(i-last)/float64(fps) > maxGapS+1e-9 {
			result = append(result, segment{float64(start) / float64(fps), float64(last) / float64(fps)})
			start = -1
		}
		if start < 0 {
			start = i
		}
		last = i
	}
	if start >= 0 {
		result = append(result, segment{float64(start) / float64(fps), float64(last) / float64(fps)})
	}
	return result
}

func (s scene) render(i int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, s.width, s.height))
	pix := img.Pix
	switch {
	case s.isPoison(i):
		for p := 0; p < len(pix); p += 4 {
			pix[p], pix[p+1], pix[p+2], pix[p+3] = poisonGrey, poisonGrey, poisonGrey, 255
		}
		return img
	case s.noise > 0:
		rng := rand.New(rand.NewPCG(uint64(len(s.name)), uint64(i)+1))
		for p := 0; p < len(pix); p += 4 {
			v := uint8(noiseBaseLevel + rng.IntN(2*s.noise+1) - s.noise)
			pix[p], pix[p+1], pix[p+2], pix[p+3] = v, v, v, 255
		}
	default:
		for p := 3; p < len(pix); p += 4 {
			pix[p] = 255
		}
	}
	for _, b := range s.boxesAt(i) {
		c := labelChannels[b.Label]
		for y := b.Y; y < b.Y+b.H; y++ {
			row := pix[y*img.Stride:]
			for x := b.X; x < b.X+b.W; x++ {
				for ch := range 3 {
					if c[ch] > 0 {
						row[4*x+ch] = c[ch]
					}
				}
			}
		}
	}
	return img
}

var (
	encodedMu sync.Mutex
	encoded   = map[string][]byte{}
)

func (s scene) bytes() []byte {
	encodedMu.Lock()
	defer encodedMu.Unlock()
	key := "scene:" + s.name
	if data, ok := encoded[key]; ok {
		return data
	}
	data, err := encodeMJPEGAVI(s.frames, s.render, s.fps, 90)
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: encoding %s: %v", s.name, err))
	}
	encoded[key] = data
	return data
}

// write puts the scene in dir under a random name, like /tmp/x/street-apple.avi.
func (s scene) write(dir string) string {
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.avi", s.name, random.RandomWord()))
	if err := os.WriteFile(path, s.bytes(), 0o644); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: writing %s: %v", path, err))
	}
	return path
}

func (s scene) duration() float64 { return float64(s.frames) / float64(s.fps) }
