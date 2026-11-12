package internal

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScenesFollowTheRules(t *testing.T) {
	for _, s := range allScenes {
		for i := range s.frames {
			seen := map[string]bool{}
			for _, o := range s.objects {
				b, ok := o.placedAt(i)
				if !ok {
					continue
				}
				if seen[b.Label] {
					t.Fatalf("%s frame %d: two %s objects", s.name, i, b.Label)
				}
				seen[b.Label] = true
				if b.X < 0 || b.Y < 0 || b.X+b.W > s.width || b.Y+b.H > s.height {
					t.Fatalf("%s frame %d: %v leaves the frame", s.name, i, b)
				}
			}
		}
	}
}

// The golden files agree with the scenes: the model finds every object where
// it was drawn and finds nothing that wasn't drawn.
func TestGoldenMatchesScenes(t *testing.T) {
	for _, s := range allScenes {
		for i := range s.frames {
			placed := map[string]box{}
			if !s.isPoison(i) {
				for _, o := range s.objects {
					if b, ok := o.placedAt(i); ok {
						placed[b.Label] = b
					}
				}
			}
			found := map[string]box{}
			for _, b := range s.boxesAt(i) {
				found[b.Label] = b
			}
			if len(found) != len(placed) {
				t.Fatalf("%s frame %d: the model finds %v, the scene has %v", s.name, i, found, placed)
			}
			for label, p := range placed {
				f, ok := found[label]
				if !ok || f.iou(p) < 0.5 {
					t.Fatalf("%s frame %d: %s drawn at %v, found at %v", s.name, i, label, p, f)
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
		{"street person+car", groupFrames(sceneStreet.overlapFrames("person", "car", 1), 10, 0.5), []segment{{4.8, 7.1}}},
		{"street person, every 5th frame", groupFrames(sceneStreet.presenceFrames("person", 5), 10, 0.5), []segment{{1.0, 8.0}}},
		{"poison person", groupFrames(scenePoison.presenceFrames("person", 1), 10, 0.5), []segment{{0.0, 5.9}}},
		{"hd person", groupFrames(sceneHD.presenceFrames("person", 1), 10, 0.5), []segment{{1.0, 19.0}}},
		{"hd person+car", groupFrames(sceneHD.overlapFrames("person", "car", 1), 10, 0.5), []segment{{7.7, 13.0}}},
	}
	for _, tc := range cases {
		if !sameSegments(tc.got, tc.expect) {
			t.Errorf("%s: got %s, want %s", tc.name, formatSegments(tc.got), formatSegments(tc.expect))
		}
	}

	long := groupFrames(sceneLong.presenceFrames("person", 1), 5, 0.5)
	if len(long) != 10 || !sameSegments(long[:1], []segment{{10, 20}}) || !sameSegments(long[9:], []segment{{550, 560}}) {
		t.Errorf("long person: got %s", formatSegments(long))
	}
	if cars := groupFrames(sceneLong.presenceFrames("car", 1), 5, 0.5); len(cars) != 5 {
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

// Every scene's AVI holds the frames it should, and poison frames stay uniform grey after JPEG.
func TestScenesSurviveJPEG(t *testing.T) {
	for _, s := range allScenes {
		frames := aviFrames(t, s.bytes())
		if len(frames) != s.frames {
			t.Fatalf("%s: %d frames in the AVI, want %d", s.name, len(frames), s.frames)
		}
		for _, i := range s.poison {
			img, err := jpeg.Decode(bytes.NewReader(frames[i]))
			if err != nil {
				t.Fatalf("%s frame %d: %v", s.name, i, err)
			}
			r, g, b, _ := img.At(s.width/2, s.height/2).RGBA()
			if d := int(r>>8) - poisonGrey; d < -3 || d > 3 || r != g || g != b {
				t.Fatalf("%s frame %d: poison frame is not uniform grey", s.name, i)
			}
		}
	}
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

// FRAMEWISE_DUMP_SCENES=<dir> go test -run TestDumpScenes ./internal writes
// every scene to dir, so tester/golden/make_golden.py can run the model on them.
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

// Where the tester runs the detector at another size (the model server's 512,
// the fast profile's 320), it still expects the labels it expects at 640, each
// boundary within the frame the tester allows anyway.
func TestSizesAgree(t *testing.T) {
	for _, s := range allScenes {
		for size := range goldenFor(s).DetectAt {
			for _, label := range []string{"person", "car", "bicycle"} {
				want := groupFrames(s.presenceFrames(label, 1), s.fps, 0.5)
				got := groupFrames(s.presenceFramesAt(size, label, 1), s.fps, 0.5)
				ok := len(got) == len(want)
				for k := 0; ok && k < len(got); k++ {
					frame := 1/float64(s.fps) + 1e-9
					ok = math.Abs(got[k].Start-want[k].Start) <= frame && math.Abs(got[k].End-want[k].End) <= frame
				}
				if !ok {
					t.Errorf("%s %s at %d: %s, at 640: %s", s.name, label, size, formatSegments(got), formatSegments(want))
				}
			}
		}
	}
}

// The reference itself passes the comparison the stages make, at every size.
func TestReferencePassesCompare(t *testing.T) {
	for _, s := range allScenes {
		sizes := []int{640}
		for size := range goldenFor(s).DetectAt {
			sizes = append(sizes, size)
		}
		for _, size := range sizes {
			var got []storedBox
			for i := range s.frames {
				boxes := s.boxesAtSize(size, i)
				sort.SliceStable(boxes, func(a, b int) bool { return boxes[a].Label < boxes[b].Label })
				for _, b := range boxes {
					got = append(got, storedBox{float64(i) / float64(s.fps), b})
				}
			}
			if err := compareDetectionsAt(got, s, size, 0, s.frames-1, 0.85); err != nil {
				t.Errorf("%s at %d: %v", s.name, size, err)
			}
			lo, hi := s.rowCount(size, 1)
			if len(got) < lo || len(got) > hi {
				t.Errorf("%s at %d: %d rows, outside [%d, %d]", s.name, size, len(got), lo, hi)
			}
		}
	}
}
