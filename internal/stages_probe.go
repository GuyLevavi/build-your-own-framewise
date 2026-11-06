package internal

import (
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// probe runs `probe path extra...` and returns the parsed JSON object.
func probe(harness *test_case_harness.TestCaseHarness, path string, extra ...string) (map[string]any, error) {
	result, err := runCLI(harness, append([]string{"probe", path}, extra...)...)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	return jsonObject(result.Stdout)
}

func expectSize(object map[string]any, c clip) error {
	width, err := intField(object, "width")
	if err != nil {
		return err
	}
	height, err := intField(object, "height")
	if err != nil {
		return err
	}
	if width != int64(c.width) || height != int64(c.height) {
		return fmt.Errorf("expected %dx%d, got %dx%d", c.width, c.height, width, height)
	}
	return nil
}

func expectTiming(object map[string]any, c clip) error {
	frameCount, err := intField(object, "frame_count")
	if err != nil {
		return err
	}
	if frameCount != int64(c.frames) {
		return fmt.Errorf("expected frame_count %d, got %d", c.frames, frameCount)
	}
	fps, err := floatField(object, "fps")
	if err != nil {
		return err
	}
	if math.Abs(fps-float64(c.fps)) > 0.01 {
		return fmt.Errorf("expected fps %d (within 0.01), got %v", c.fps, fps)
	}
	duration, err := floatField(object, "duration_s")
	if err != nil {
		return err
	}
	if math.Abs(duration-c.duration()) > 0.01 {
		return fmt.Errorf("expected duration_s %.2f (frame_count / fps, within 0.01), got %v", c.duration(), duration)
	}
	return nil
}

func testProbeSize(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, c := range []clip{clipSquare, clipBig} {
		object, err := probe(harness, c.write(dir))
		if err != nil {
			return err
		}
		if err := expectSize(object, c); err != nil {
			return err
		}
		harness.Logger.Successf("✓ %dx%d", c.width, c.height)
	}
	return nil
}

func testProbeTiming(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, c := range []clip{clipSquare, clipBig} {
		object, err := probe(harness, c.write(dir))
		if err != nil {
			return err
		}
		if err := expectSize(object, c); err != nil {
			return err
		}
		if err := expectTiming(object, c); err != nil {
			return err
		}
		harness.Logger.Successf("✓ %dx%d, %d fps, %d frames, %.1f s", c.width, c.height, c.fps, c.frames, c.duration())
	}
	return nil
}

func testProbeMissing(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, ext := range []string{".mp4", ".avi"} {
		path := filepath.Join(dir, random.RandomWord()+ext)
		result, err := runCLI(harness, "probe", path)
		if err != nil {
			return err
		}
		if err := expectCleanFailure(result, filepath.Base(path)); err != nil {
			return err
		}
		harness.Logger.Successf("✓ exit code %d, stderr names the file", result.ExitCode)
	}
	return nil
}

func testProbeCorrupt(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	bad := []struct {
		prefix, ext string
		data        []byte
	}{
		{"garbage", ".mp4", []byte(strings.Repeat("this is not a video, just some text\n", 40))},
		{"empty", ".avi", nil},
		{"truncated", ".avi", clipSquare.bytes()[:300]},
	}
	for _, b := range bad {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s%s", b.prefix, random.RandomWord(), b.ext))
		if err := os.WriteFile(path, b.data, 0o644); err != nil {
			return err
		}
		result, err := runCLI(harness, "probe", path)
		if err != nil {
			return err
		}
		if err := expectCleanFailure(result, filepath.Base(path)); err != nil {
			return err
		}
		harness.Logger.Successf("✓ exit code %d, stderr names the file", result.ExitCode)
	}
	return nil
}

func testProbeSaveFrames(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, c := range []clip{clipSquare, clipBig} {
		every := random.RandomInt(3, 10)
		out := filepath.Join(dir, "frames-"+random.RandomWord(), c.name)
		object, err := probe(harness, c.write(dir), "--save-every", fmt.Sprint(every), "--out", out)
		if err != nil {
			return err
		}
		if err := expectTiming(object, c); err != nil {
			return err
		}

		var want []string
		for i := 0; i < c.frames; i += every {
			want = append(want, fmt.Sprintf("frame_%05d.jpg", i))
		}
		entries, err := os.ReadDir(out)
		if err != nil {
			return fmt.Errorf("expected --out %s to exist, got %v", out, err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			return fmt.Errorf("expected exactly these files in %s:\n  %s\ngot:\n  %s", out, strings.Join(want, " "), strings.Join(got, " "))
		}

		for i := 0; i < c.frames; i += every {
			name := fmt.Sprintf("frame_%05d.jpg", i)
			if err := checkSavedFrame(filepath.Join(out, name), c, i); err != nil {
				return fmt.Errorf("%s: %v", name, err)
			}
		}
		harness.Logger.Successf("✓ %d frames saved, every %d, each showing the right picture", len(want), every)
	}
	return nil
}

// checkSavedFrame decodes a JPEG and finds the white square in it.
func checkSavedFrame(path string, c clip, index int) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	img, err := jpeg.Decode(file)
	if err != nil {
		return fmt.Errorf("not a JPEG file (%v)", err)
	}
	b := img.Bounds()
	if b.Dx() != c.width || b.Dy() != c.height {
		return fmt.Errorf("expected a %dx%d picture, got %dx%d", c.width, c.height, b.Dx(), b.Dy())
	}
	sumX, sumY, n := 0, 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if luma(img, x, y) > 200 {
				sumX += x
				sumY += y
				n++
			}
		}
	}
	if n < c.side*c.side/2 {
		return fmt.Errorf("expected to see the white square, but the picture has no bright area")
	}
	x0, y0 := c.squareAt(index)
	wantX, wantY := float64(x0)+float64(c.side-1)/2, float64(y0)+float64(c.side-1)/2
	gotX, gotY := float64(sumX)/float64(n), float64(sumY)/float64(n)
	if math.Abs(gotX-wantX) > 2 || math.Abs(gotY-wantY) > 2 {
		return fmt.Errorf("expected the square of frame %d centred at (%.0f, %.0f), found it at (%.0f, %.0f): is this the right frame?", index, wantX, wantY, gotX, gotY)
	}
	return nil
}

func luma(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257
}
