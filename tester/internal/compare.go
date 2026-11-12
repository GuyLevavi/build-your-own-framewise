package internal

import (
	"fmt"
	"math"
	"sort"
)

// How a learner's boxes are compared with the golden file's (tester/golden/make_golden.py
// checks the clips against the same rules). A detection's score moves a little with how a
// frame is prepared for the model: the letterbox in the middle or a corner, one interpolation
// or another, and a 1080p frame shrunk to 320 is noisy. So a learner's box must be one the
// reference found, with at least a middling score, at any size it was recorded at; and of the
// boxes the reference found with a clear score at the learner's size, 90% must be there.
const (
	allowedScore  = 0.3 // a learner's box must be one the reference found with at least this
	requiredScore = 0.7 // a box the reference found with at least this should be reported
	maxMissing    = 0.1 // of those, this many may be missing
)

// rowsAt lists everything the detector reports for frame i when run at size (640 unless the
// golden file has the clip at another), down to the golden file's lowest score.
func (s scene) rowsAt(size, i int) []box {
	if s.isPoison(i) {
		return nil
	}
	g := goldenFor(s)
	if size == 640 {
		return g.Detect[i]
	}
	frames, ok := g.DetectAt[size]
	if !ok {
		panic(fmt.Sprintf("CodeCrafters Internal Error: no golden detections for %s at %d", s.name, size))
	}
	return frames[i]
}

// rowCount is how many rows a job that runs the detector at size on every `every`-th frame
// of s may store: at least lo (most of the boxes it must find), at most hi (those it may find).
func (s scene) rowCount(size, every int) (lo, hi int) {
	required := 0
	for i := 0; i < s.frames; i += every {
		for _, b := range s.rowsAt(size, i) {
			if b.Score >= requiredScore {
				required++
			}
			if b.Score >= allowedScore {
				hi++
			}
		}
	}
	return required - int(maxMissing*float64(required)), hi
}

// allowedAt lists the boxes a learner may report for frame i: what the detector finds with at
// least allowedScore at any size the golden file has, the most certain first.
func (s scene) allowedAt(i int) []box {
	if s.isPoison(i) {
		return nil
	}
	g := goldenFor(s)
	all := append([]box(nil), g.Detect[i]...)
	for _, frames := range g.DetectAt {
		all = append(all, frames[i]...)
	}
	var allowed []box
	for _, b := range all {
		if b.Score >= allowedScore {
			allowed = append(allowed, b)
		}
	}
	sort.SliceStable(allowed, func(a, b int) bool { return allowed[a].Score > allowed[b].Score })
	return allowed
}

// expectRowCount checks a job's number of stored rows against rowCount.
func expectRowCount(n int64, s scene, size, every int, what string) error {
	lo, hi := s.rowCount(size, every)
	if n >= int64(lo) && n <= int64(hi) {
		return nil
	}
	want := fmt.Sprintf("between %d and %d", lo, hi)
	if lo == hi {
		want = fmt.Sprintf("exactly %d", lo)
	}
	return fmt.Errorf("expected %s rows in results.detections for %s (one per box the model finds with a score of at least %.1f, per analyzed frame), found %d", want, what, minScore, n)
}

// compareFrame matches got, a learner's boxes for frame i, with what the detector finds there,
// one to one by label and IoU. It returns how many of the boxes the reference found clearly at
// size are missing, of how many.
func compareFrame(got []box, s scene, size, i int, minIoU float64) (missing, required int, err error) {
	allowed := s.allowedAt(i)
	used := make([]bool, len(allowed))
	var matched []box
	for _, g := range got {
		// The most certain of the reference's boxes in the same place: the model sometimes
		// reports an object twice, once with a low score.
		best := -1
		for k, a := range allowed {
			if !used[k] && a.Label == g.Label && a.iou(g) >= minIoU {
				best = k
				break
			}
		}
		if best < 0 {
			return 0, 0, fmt.Errorf("frame %d (%.1f s): the model finds no %s at (%d, %d, %d, %d) (IoU at least %.2f with one it finds)", i, float64(i)/float64(s.fps), g.Label, g.X, g.Y, g.W, g.H, minIoU)
		}
		used[best] = true
		matched = append(matched, allowed[best])
	}
	for _, r := range s.rowsAt(size, i) {
		if r.Score < requiredScore {
			continue
		}
		required++
		found := false
		for _, m := range matched {
			if m.Label == r.Label && m.iou(r) >= minIoU {
				found = true
			}
		}
		if !found {
			missing++
		}
	}
	return missing, required, nil
}

// compareDetectionsAt checks got, sorted by time then label, against frames [from, to] of s
// with the detector run at size.
func compareDetectionsAt(got []storedBox, s scene, size, from, to int, minIoU float64) error {
	byFrame := map[int][]box{}
	for k, g := range got {
		if k > 0 && (g.t < got[k-1].t || g.t == got[k-1].t && g.Label < got[k-1].Label) {
			return fmt.Errorf("expected detections sorted by t_s, then label: %s comes after %s", g, got[k-1])
		}
		i := int(math.Round(g.t * float64(s.fps)))
		if math.Abs(g.t-float64(i)/float64(s.fps)) > 1e-3 || i < from || i > to {
			return fmt.Errorf("expected detections at frame times (a multiple of %g s) from %g to %g s, got %s", 1/float64(s.fps), float64(from)/float64(s.fps), float64(to)/float64(s.fps), g)
		}
		byFrame[i] = append(byFrame[i], g.box)
	}
	missing, required := 0, 0
	var first string
	for i := from; i <= to && i < s.frames; i++ {
		m, r, err := compareFrame(byFrame[i], s, size, i, minIoU)
		if err != nil {
			return err
		}
		if m > 0 && first == "" {
			first = fmt.Sprintf("frame %d (%.1f s)", i, float64(i)/float64(s.fps))
		}
		missing += m
		required += r
	}
	if float64(missing) > maxMissing*float64(required) {
		return fmt.Errorf("expected the boxes the model finds clearly (score at least %.1f at input size %d), %d of them, all but a few; %d are missing, the first in %s", requiredScore, size, required, missing, first)
	}
	return nil
}
