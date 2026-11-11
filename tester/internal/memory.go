package internal

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// groupRSS is the resident memory, in bytes, of every process in process
// group pgid: the program and everything it started (the tester starts each
// program in a group of its own). Processes that vanish while it reads are skipped.
func groupRSS(pgid int) int64 {
	entries, _ := os.ReadDir("/proc")
	var total int64
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		// Fields after the command, which is in parentheses and may contain spaces.
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 3 || fields[2] != strconv.Itoa(pgid) {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					kb, _ := strconv.ParseInt(f[1], 10, 64)
					total += kb * 1024
				}
			}
		}
	}
	return total
}

// rssSampler records the peak of groupRSS(pgid), sampled every 50 ms.
type rssSampler struct {
	pgid int
	mu   sync.Mutex
	peak int64
	at   time.Duration
	stop chan struct{}
	done chan struct{}
}

func sampleRSS(pgid int) *rssSampler {
	s := &rssSampler{pgid: pgid, stop: make(chan struct{}), done: make(chan struct{})}
	start := time.Now()
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			rss := groupRSS(pgid)
			s.mu.Lock()
			if rss > s.peak {
				s.peak, s.at = rss, time.Since(start)
			}
			s.mu.Unlock()
			select {
			case <-s.stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return s
}

// finish stops sampling and returns the peak and when it happened.
func (s *rssSampler) finish() (int64, time.Duration) {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak, s.at
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }
