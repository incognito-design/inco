package inco

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRelease verifies that Release copies shadow files alongside originals
// with the correct build tags and generated-code header.
func TestRelease(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func main() {
	x := 42
	// @require x > 0
	_ = x
}
`,
	})

	// 1. Generate overlay.
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) == 0 {
		t.Fatal("expected overlay entries after gen")
	}

	// 2. Release.
	Release(dir)

	// 3. Check released file exists.
	releasePath := filepath.Join(dir, "main_inco.go")
	releaseContent, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatalf("released file not found: %v", err)
	}
	rc := string(releaseContent)

	// Must have generated-code header.
	if !strings.HasPrefix(rc, releaseHeader) {
		t.Error("released file missing generated-code header")
	}
	// Must have //go:build inco.
	if !strings.Contains(rc, "//go:build inco") {
		t.Error("released file missing //go:build inco tag")
	}
	// Must NOT have //go:build !inco (stripped from shadow).
	if strings.Contains(rc, "//go:build !inco") {
		t.Error("released file should not contain //go:build !inco")
	}
	// Must contain the guard (if !(x > 0) { panic(...) }).
	if !strings.Contains(rc, "if !(x > 0)") {
		t.Error("released file missing injected guard")
	}

	// 4. Check original file got //go:build !inco.
	origContent, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	oc := string(origContent)
	if !strings.HasPrefix(oc, excludeBuildTag) {
		t.Error("original file missing //go:build !inco tag")
	}
}

// TestRelease_Idempotent ensures running Release twice doesn't double-tag
// the original file.
func TestRelease_Idempotent(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func main() {
	// @require true
}
`,
	})

	e := NewEngine(dir)
	e.Run()

	Release(dir)
	Release(dir) // second call

	origContent, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	oc := string(origContent)

	// Should have exactly one //go:build !inco.
	count := strings.Count(oc, "//go:build !inco")
	if count != 1 {
		t.Errorf("expected 1 //go:build !inco, got %d", count)
	}
}

// TestReleaseClean removes released files and restores originals.
func TestReleaseClean(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func main() {
	// @require true
}
`,
	})

	e := NewEngine(dir)
	e.Run()

	Release(dir)

	// Verify released file exists.
	releasePath := filepath.Join(dir, "main_inco.go")
	if _, err := os.Stat(releasePath); err != nil {
		t.Fatal("released file should exist before clean")
	}

	// Clean.
	ReleaseClean(dir)

	// Released file should be gone.
	if _, err := os.Stat(releasePath); !os.IsNotExist(err) {
		t.Error("released file should be removed after clean")
	}

	// Original should no longer have //go:build !inco.
	origContent, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(origContent), "//go:build !inco") {
		t.Error("original should have //go:build !inco removed after clean")
	}
}

// TestReleasePathFor tests the helper function.
func TestReleasePathFor(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/a/b/main.go", "/a/b/main_inco.go"},
		{"/src/engine.go", "/src/engine_inco.go"},
	}
	for _, tt := range tests {
		got := releasePathFor(tt.input)
		if got != tt.want {
			t.Errorf("releasePathFor(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestScanner_SkipsIncoFiles verifies the engine skips _inco.go files.
func TestScanner_SkipsIncoFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func main() {
	// @require true
}
`,
		// This _inco.go file should be ignored by the scanner.
		"main_inco.go": `package main

func init() {
	// @require false
}
`,
	})

	e := NewEngine(dir)
	e.Run()

	// Only main.go should appear in the overlay (not main_inco.go).
	if len(e.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}
	for orig := range e.Overlay.Replace {
		if strings.HasSuffix(orig, "_inco.go") {
			t.Error("scanner should not process _inco.go files")
		}
	}
}
