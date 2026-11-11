package internal

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// The tester's hook in the learner's processes (hook/sitecustomize.py, read its
// header): it logs every model call to FRAMEWISE_TRACE and aborts the process
// that runs the model on a uniform grey frame. Every program the tester starts
// inherits PYTHONPATH, so every Python process it starts, and theirs, loads it.
//
//go:embed hook/sitecustomize.py
var hookSource []byte

// installHook writes the hook to a directory of its own and puts that first on
// PYTHONPATH; the returned function removes it.
func installHook() func() {
	dir, err := os.MkdirTemp("", "framewise-hook-")
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
	if err := os.WriteFile(filepath.Join(dir, "sitecustomize.py"), hookSource, 0o644); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
	path := dir
	if old := os.Getenv("PYTHONPATH"); old != "" {
		path += string(os.PathListSeparator) + old
	}
	os.Setenv("PYTHONPATH", path)
	return func() { os.RemoveAll(dir) }
}
