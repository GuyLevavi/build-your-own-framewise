package internal

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// expectDetectionsWithin compares a time range of a job's stored detections with sc, positions within tolerance px.
func expectDetectionsWithin(sv *service, jobID string, sc scene, size int, fromS, toS float64, minIoU float64) error {
	r, err := sv.get(fmt.Sprintf("/jobs/%s/detections?from_s=%g&to_s=%g", jobID, fromS, toS), nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return err
	}
	got, err := detectionsField(r.object)
	if err != nil {
		return err
	}
	from, to := int(fromS*float64(sc.fps)+0.5), int(toS*float64(sc.fps)+0.5)
	if err := compareDetectionsAt(got, sc, size, from, to, minIoU); err != nil {
		return fmt.Errorf("%v. Boxes are stored in the coordinates of the original video, whatever size you run the detector at", err)
	}
	sv.harness.Logger.Successf("✓ %d detections between %g and %g s, in %dx%d coordinates", len(got), fromS, toS, sc.width, sc.height)
	return nil
}

func availableCPUs() int {
	n := runtime.NumCPU()
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err != nil {
		return n
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[0] == "max" {
		return n
	}
	quota, err1 := strconv.ParseFloat(fields[0], 64)
	period, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil || period <= 0 {
		return n
	}
	return max(1, min(n, int(quota/period)))
}
