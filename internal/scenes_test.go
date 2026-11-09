package internal

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestScenesFollowTheRules(t *testing.T) {
	for _, s := range allScenes {
		for i := range s.frames {
			seen := map[string]bool{}
			for _, b := range s.boxesAt(i) {
				if seen[b.Label] {
					t.Fatalf("%s frame %d: two %s boxes", s.name, i, b.Label)
				}
				seen[b.Label] = true
				if b.X < 0 || b.Y < 0 || b.X+b.W > s.width || b.Y+b.H > s.height {
					t.Fatalf("%s frame %d: %v leaves the frame", s.name, i, b)
				}
				if _, ok := labelChannels[b.Label]; !ok {
					t.Fatalf("%s: unknown label %q", s.name, b.Label)
				}
			}
		}
	}
}

// The stage texts will quote these numbers; keep them and the scenes in step.
func TestSceneNumbers(t *testing.T) {
	cases := []struct {
		name   string
		got    []segment
		expect []segment
	}{
		{"street person", groupFrames(sceneStreet.presenceFrames("person", 1), 10, 0.5), []segment{{1.0, 8.0}}},
		{"street car", groupFrames(sceneStreet.presenceFrames("car", 1), 10, 0.5), []segment{{4.0, 11.0}}},
		{"street person+car", groupFrames(sceneStreet.overlapFrames("person", "car", 1), 10, 0.5), []segment{{5.3, 6.6}}},
		{"street person, every 5th frame", groupFrames(sceneStreet.presenceFrames("person", 5), 10, 0.5), []segment{{1.0, 8.0}}},
		{"poison person", groupFrames(scenePoison.presenceFrames("person", 1), 10, 0.5), []segment{{0.0, 5.9}}},
		{"hd person", groupFrames(sceneHD.presenceFrames("person", 1), 25, 0.5), []segment{{1.0, 19.0}}},
		{"hd person+car", groupFrames(sceneHD.overlapFrames("person", "car", 1), 25, 0.5), []segment{{9.04, 12.32}}},
	}
	for _, tc := range cases {
		if !sameSegments(tc.got, tc.expect) {
			t.Errorf("%s: got %s, want %s", tc.name, formatSegments(tc.got), formatSegments(tc.expect))
		}
	}

	long := groupFrames(sceneLong.presenceFrames("person", 1), 10, 0.5)
	if len(long) != 10 || !sameSegments(long[:1], []segment{{10, 20}}) || !sameSegments(long[9:], []segment{{550, 560}}) {
		t.Errorf("long person: got %s", formatSegments(long))
	}
	if cars := groupFrames(sceneLong.presenceFrames("car", 1), 10, 0.5); len(cars) != 5 {
		t.Errorf("long car: got %s", formatSegments(cars))
	}
	if o := sceneLong.overlapFrames("person", "car", 1); len(o) != 0 {
		t.Errorf("long: person and car should never overlap, got %d frames", len(o))
	}
	if sceneLong.duration() != 600 || sceneHD.width != 1920 || sceneHD.height != 1080 {
		t.Error("the stage texts quote a 10-minute long clip and a 1080p clip")
	}
}

func sameSegments(a, b []segment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i].Start-b[i].Start) > 1e-9 || math.Abs(a[i].End-b[i].End) > 1e-9 {
			return false
		}
	}
	return true
}

// Decode every scene's frames the way fwdetect sees them (JPEG, so colours
// blur at the edges) and check that thresholding each label's channel gives
// its box back within 2 pixels, and that poison frames stay uniform grey.
func TestScenesSurviveJPEG(t *testing.T) {
	for _, s := range allScenes {
		frames := aviFrames(t, s.bytes())
		if len(frames) != s.frames {
			t.Fatalf("%s: %d frames in the AVI, want %d", s.name, len(frames), s.frames)
		}
		step := max(1, s.frames/40)
		for i := 0; i < s.frames; i += step {
			img, err := jpeg.Decode(bytes.NewReader(frames[i]))
			if err != nil {
				t.Fatalf("%s frame %d: %v", s.name, i, err)
			}
			if s.isPoison(i) {
				r, g, b, _ := img.At(s.width/2, s.height/2).RGBA()
				if d := int(r>>8) - poisonGrey; d < -3 || d > 3 || r != g || g != b {
					t.Fatalf("%s frame %d: poison frame is not uniform grey", s.name, i)
				}
				continue
			}
			want := map[string]box{}
			for _, b := range s.boxesAt(i) {
				want[b.Label] = b
			}
			for label, ch := range labelChannels {
				got, ok := thresholdBox(img, ch)
				w, present := want[label]
				if ok != present {
					t.Fatalf("%s frame %d: %s found=%v, want %v", s.name, i, label, ok, present)
				}
				if ok && (abs(got.X-w.X) > 2 || abs(got.Y-w.Y) > 2 || abs(got.W-w.W) > 2 || abs(got.H-w.H) > 2) {
					t.Fatalf("%s frame %d: %s box %v after JPEG, want %v", s.name, i, label, got, w)
				}
			}
		}
	}
}

// thresholdBox is the bounding box of the pixels whose label channel is
// above 160: fwdetect's own rule.
func thresholdBox(img image.Image, ch [3]uint8) (box, bool) {
	c := 0
	for k := range 3 {
		if ch[k] > 0 {
			c = k
		}
	}
	b := img.Bounds()
	x0, y0, x1, y1 := b.Max.X, b.Max.Y, -1, -1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			v := [3]uint32{r >> 8, g >> 8, bl >> 8}[c]
			if v > 160 {
				x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			}
		}
	}
	if x1 < 0 {
		return box{}, false
	}
	return box{X: x0, Y: y0, W: x1 - x0 + 1, H: y1 - y0 + 1}, true
}

// aviFrames reads the JPEGs back out of an AVI's movi list, in order.
func aviFrames(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var frames [][]byte
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		size := int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		switch id {
		case "LIST":
			p += 12 // descend into the list
			continue
		case "00dc":
			frames = append(frames, data[p+8:p+8+size])
		}
		p += 8 + size + size%2
	}
	return frames
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// FRAMEWISE_DUMP_SCENES=<dir> go test -run TestDumpScenes ./internal writes
// every scene to dir, so the course repo can check fwdetect against them.
func TestDumpScenes(t *testing.T) {
	dir := os.Getenv("FRAMEWISE_DUMP_SCENES")
	if dir == "" {
		t.Skip("set FRAMEWISE_DUMP_SCENES to a directory to write the scenes there")
	}
	for _, s := range allScenes {
		if err := os.WriteFile(filepath.Join(dir, s.name+".avi"), s.bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
