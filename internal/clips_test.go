package internal

import (
	"bytes"
	"math"
	"testing"
)

// The stage descriptions quote these numbers; keep them and the clips in step.
func TestClipsMatchStageDescriptions(t *testing.T) {
	cases := []struct {
		name   string
		c      clip
		gap    float64
		expect []segment
	}{
		{"mo1 square", clipSquare, math.Inf(1), []segment{{1.0, 2.9}}},
		{"mo1 still", clipStill, math.Inf(1), nil},
		{"mo2 two bursts", clipTwoMoves, 0.1, []segment{{0.5, 1.2}, {2.5, 3.4}}},
		{"mo3 pause", clipPause, 0.5, []segment{{0.5, 2.7}}},
		{"mo3 two bursts", clipTwoMoves, 0.5, []segment{{0.5, 1.2}, {2.5, 3.4}}},
		{"mo4 noisy square", clipNoisySquare, 0.5, []segment{{0.8, 2.1}}},
		{"cf1 pause split", clipPause, 0.25, []segment{{0.5, 1.4}, {1.8, 2.7}}},
	}
	for _, tc := range cases {
		got := tc.c.segments(tc.gap)
		if len(got) != len(tc.expect) {
			t.Fatalf("%s: got %s, want %s", tc.name, formatSegments(got), formatSegments(tc.expect))
		}
		for i := range got {
			if math.Abs(got[i].Start-tc.expect[i].Start) > 1e-9 || math.Abs(got[i].End-tc.expect[i].End) > 1e-9 {
				t.Fatalf("%s: got %s, want %s", tc.name, formatSegments(got), formatSegments(tc.expect))
			}
		}
	}
	if clipSquare.frames != 40 || clipSquare.fps != 10 || clipBig.fps != 15 {
		t.Fatal("pb2 quotes a 40-frame 10 fps clip and a 15 fps clip")
	}
}

func TestClipsStayInFrame(t *testing.T) {
	for _, c := range []clip{clipSquare, clipBig, clipStill, clipTwoMoves, clipPause, clipNoisyStill, clipNoisySquare, clipLong} {
		x, y := c.squareAt(c.frames - 1)
		if x+c.side > c.width || y+c.side > c.height {
			t.Errorf("%s: the square leaves the frame", c.name)
		}
		data := c.bytes()
		if !bytes.HasPrefix(data, []byte("RIFF")) || string(data[8:12]) != "AVI " {
			t.Errorf("%s: not an AVI file", c.name)
		}
	}
}
