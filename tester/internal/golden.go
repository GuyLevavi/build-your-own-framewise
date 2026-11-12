package internal

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// What the course's model reports on every frame of every scene, recorded by
// tester/golden/make_golden.py (see its header): the detector's boxes in the
// video's pixels (at 640, and at the other sizes the tester runs it at on that
// clip), each with its score.
//
//go:embed golden
var goldenFiles embed.FS

type golden struct {
	Detect   [][]box
	DetectAt map[int][][]box // by input size, other than 640
}

type goldenFile struct {
	Scene    string               `json:"scene"`
	Frames   int                  `json:"frames"`
	Detect   [][][]any            `json:"detect"`    // per frame: [label, score, x, y, w, h]
	DetectAt map[string][][][]any `json:"detect_at"` // by size, as detect
}

var (
	goldenMu    sync.Mutex
	goldenCache = map[string]*golden{}
)

func goldenFor(s scene) *golden {
	goldenMu.Lock()
	defer goldenMu.Unlock()
	if g, ok := goldenCache[s.name]; ok {
		return g
	}
	g, err := loadGolden(s)
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: golden file for %s: %v", s.name, err))
	}
	goldenCache[s.name] = g
	return g
}

func loadGolden(s scene) (*golden, error) {
	data, err := goldenFiles.ReadFile("golden/" + s.name + ".json.gz")
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var f goldenFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.Frames != s.frames || len(f.Detect) != s.frames {
		return nil, fmt.Errorf("it has %d frames, the scene %d: run tester/golden/make_golden.py", len(f.Detect), s.frames)
	}
	g := &golden{DetectAt: map[int][][]box{}}
	if g.Detect, err = goldenBoxes(f.Detect, s.frames); err != nil {
		return nil, err
	}
	for size, frames := range f.DetectAt {
		n, err := strconv.Atoi(size)
		if err != nil {
			return nil, fmt.Errorf("detect_at: bad size %q", size)
		}
		if g.DetectAt[n], err = goldenBoxes(frames, s.frames); err != nil {
			return nil, fmt.Errorf("detect_at %s: %v", size, err)
		}
	}
	return g, nil
}

func goldenBoxes(frames [][][]any, n int) ([][]box, error) {
	if len(frames) != n {
		return nil, fmt.Errorf("%d frames, the scene has %d", len(frames), n)
	}
	result := make([][]box, n)
	for i, rows := range frames {
		for _, r := range rows {
			if len(r) != 6 {
				return nil, fmt.Errorf("frame %d: bad row %v", i, r)
			}
			result[i] = append(result[i], box{
				Label: r[0].(string), Score: r[1].(float64),
				X: int(r[2].(float64)), Y: int(r[3].(float64)), W: int(r[4].(float64)), H: int(r[5].(float64)),
			})
		}
	}
	return result, nil
}
