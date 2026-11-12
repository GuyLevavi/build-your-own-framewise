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

// A scene is a synthetic clip for the course's model, YOLO26n: real people and
// cars, cut out of photos (assets/), that move along straight lines on a dark
// (or dark, noisy) background, or a car parked in front of the camera for the
// classifier. The tester can't know what a neural network will report from
// the geometry alone, so what the model finds in every frame is recorded in
// golden/<scene>.json by tester/golden/make_golden.py, which runs the course's
// reference pipeline (the .pt with Ultralytics) on the decoded AVI; the
// tester compares learners' results against those, within tolerances.
//
// Rules the stage texts rely on (scenes_test.go checks them):
//   - at most one object of each label is placed in any frame, and the model
//     finds it there with a score well above 0.5, and nothing else above 0.5;
//   - every object stays inside the frame;
//   - a poison frame is uniform mid-grey: the tester's hook in your processes
//     aborts whatever process runs the model on it (SIGABRT), the way a native
//     library bug kills a worker.
type scene struct {
	name          string
	width, height int
	fps, frames   int
	objects       []object
	noise         int   // amplitude of per-pixel noise around dark grey 40; 0 means flat grey 40
	poison        []int // frames rendered as uniform grey 128
}

type object struct {
	label  string // the COCO label the detector reports; sprite name in assets/ unless sprite is set
	sprite string
	h      int // height in pixels; the width follows the sprite's aspect ratio
	path   []keyframe
}

// keyframe puts the object's top-left corner at (x, y) in frame `frame`;
// between keyframes it moves linearly.
type keyframe struct{ frame, x, y int }

// box is a detection in the video's pixels.
type box struct {
	Label      string
	X, Y, W, H int
	Score      float64
}

func (b box) intersects(o box) bool {
	return b.X < o.X+o.W && o.X < b.X+b.W && b.Y < o.Y+o.H && o.Y < b.Y+b.H
}

// iou is intersection over union, 0 for boxes that don't touch.
func (b box) iou(o box) float64 {
	ix := max(0, min(b.X+b.W, o.X+o.W)-max(b.X, o.X))
	iy := max(0, min(b.Y+b.H, o.Y+o.H)-max(b.Y, o.Y))
	inter := float64(ix * iy)
	union := float64(b.W*b.H+o.W*o.H) - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

const (
	poisonGrey     = 128
	backgroundGrey = 40
	// minScore is the score from which a detection counts, as the stage texts say.
	minScore = 0.5
)

func passes(label string, h, startFrame, endFrame, x0, x1, y int) object {
	return object{label: label, h: h, path: []keyframe{{startFrame, x0, y}, {endFrame, x1, y}}}
}

var (
	// sceneStreet: a person walks right while a car drives left; they cross.
	sceneStreet = scene{name: "street", width: 640, height: 360, fps: 10, frames: 120, objects: []object{
		passes("car", 110, 40, 110, 430, 10, 110),
		passes("person", 150, 10, 80, 20, 500, 200),
	}}
	// sceneNoisyStreet is sceneStreet on a noisy background.
	sceneNoisyStreet = func() scene { s := sceneStreet; s.name = "noisy_street"; s.noise = 8; return s }()
	// sceneLong: ten minutes at 320x180 and 5 fps; a person crosses every minute and a car every two.
	sceneLong = func() scene {
		s := scene{name: "long_street", width: 320, height: 180, fps: 5, frames: 3000}
		for k := range 10 {
			s.objects = append(s.objects, passes("person", 90, k*300+50, k*300+100, 10, 240, 60))
		}
		for k := range 5 {
			s.objects = append(s.objects, passes("car", 55, k*600+200, k*600+250, 200, 10, 115))
		}
		return s
	}()
	// sceneHD: twenty seconds of 1080p at 10 fps, for the performance stages.
	sceneHD = scene{name: "hd_street", width: 1920, height: 1080, fps: 10, frames: 200, objects: []object{
		passes("car", 320, 40, 180, 1300, 60, 280),
		passes("person", 450, 10, 190, 100, 1550, 580),
	}}
	// scenePoison: a person walks across, and frame 30 is a dropped feed that kills the process running the model.
	scenePoison = scene{name: "poison", width: 640, height: 360, fps: 10, frames: 60, poison: []int{30}, objects: []object{
		passes("person", 150, 0, 59, 20, 500, 150),
	}}
	// sceneParked: a sports car parks in front of the camera for four seconds; for the classifier.
	sceneParked = scene{name: "parked", width: 640, height: 360, fps: 10, frames: 80, objects: []object{
		{label: "car", sprite: "sports_car", h: 300, path: []keyframe{{20, 70, 30}, {59, 70, 30}}},
	}}

	allScenes = []scene{sceneStreet, sceneNoisyStreet, sceneLong, sceneHD, scenePoison, sceneParked}
)

func (o object) spriteName() string {
	if o.sprite != "" {
		return o.sprite
	}
	return o.label
}

// placedAt is where the object's sprite is drawn in frame i, if it is there.
// It is where the object is, not what the model reports: see boxesAt.
func (o object) placedAt(i int) (box, bool) {
	first, last := o.path[0], o.path[len(o.path)-1]
	if i < first.frame || i > last.frame {
		return box{}, false
	}
	w := spriteWidth(o.spriteName(), o.h)
	for k := 0; k+1 < len(o.path); k++ {
		a, b := o.path[k], o.path[k+1]
		if i >= a.frame && i <= b.frame {
			span := b.frame - a.frame
			if span == 0 {
				return box{Label: o.label, X: a.x, Y: a.y, W: w, H: o.h}, true
			}
			t := i - a.frame
			return box{Label: o.label, X: a.x + (b.x-a.x)*t/span, Y: a.y + (b.y-a.y)*t/span, W: w, H: o.h}, true
		}
	}
	return box{Label: o.label, X: first.x, Y: first.y, W: w, H: o.h}, true
}

func (s scene) isPoison(i int) bool {
	for _, p := range s.poison {
		if p == i {
			return true
		}
	}
	return false
}

// boxesAt lists what the detector reports for frame i with a score of at
// least minScore, from the golden file (none on a poison frame).
func (s scene) boxesAt(i int) []box {
	return s.boxesAtSize(640, i)
}

// boxesAtSize is boxesAt with the detector run at size.
func (s scene) boxesAtSize(size, i int) []box {
	var boxes []box
	for _, b := range s.rowsAt(size, i) {
		if b.Score >= minScore {
			boxes = append(boxes, b)
		}
	}
	return boxes
}

// classAt is the classifier's top class for frame i, if its probability is at
// least minScore.
func (s scene) classAt(i int) (string, bool) {
	if s.isPoison(i) {
		return "", false
	}
	c := goldenFor(s).Classify[i]
	if c.Score < minScore {
		return "", false
	}
	return c.Label, true
}

// presenceFrames are the frames, out of those sampled every `every` frames
// starting at 0, in which label is visible.
func (s scene) presenceFrames(label string, every int) []int {
	return s.presenceFramesAt(640, label, every)
}

// presenceFramesAt is presenceFrames with the detector run at size.
func (s scene) presenceFramesAt(size int, label string, every int) []int {
	var hits []int
	for i := 0; i < s.frames; i += every {
		for _, b := range s.boxesAtSize(size, i) {
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
			v := uint8(backgroundGrey + rng.IntN(2*s.noise+1) - s.noise)
			pix[p], pix[p+1], pix[p+2], pix[p+3] = v, v, v, 255
		}
	default:
		for p := 0; p < len(pix); p += 4 {
			pix[p], pix[p+1], pix[p+2], pix[p+3] = backgroundGrey, backgroundGrey, backgroundGrey, 255
		}
	}
	// Later objects are drawn over earlier ones.
	for _, o := range s.objects {
		if b, ok := o.placedAt(i); ok {
			drawSprite(img, spriteAt(o.spriteName(), o.h), b.X, b.Y)
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

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// classFrames are the sampled frames whose top class is label.
func (s scene) classFrames(label string, every int) []int {
	var hits []int
	for i := 0; i < s.frames; i += every {
		if c, ok := s.classAt(i); ok && c == label {
			hits = append(hits, i)
		}
	}
	return hits
}
