package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	inco "github.com/imnive-design/inco-go/internal/inco"
)

const usage = `inco — invisible constraints, invincible code.

Usage:
  inco gen [dir]           Scan source files and generate overlay
  inco build [args]        Run gen + go build -overlay
  inco test [args]         Run gen + go test -overlay
  inco run [args]          Run gen + go run -overlay
  inco audit [dir]         Contract coverage report
  inco release [--dry-run] [dir]       Copy guards into source tree
  inco release clean [dir] Remove released files and restore originals
  inco clean [dir]         Remove .inco_cache

If [dir] is omitted, the current directory is used.
`

func main() {
	defer guardPanic()

	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	switch os.Args[1] {
	case "gen":
		runGen(getDir(2))
	case "build":
		runGen(".")
		runGo("build", ".", os.Args[2:])
	case "test":
		runGen(".")
		runGo("test", ".", os.Args[2:])
	case "run":
		runGen(".")
		runGo("run", ".", os.Args[2:])
	case "audit":
		runAudit(getDir(2)).PrintReport(os.Stdout)
	case "release":
		if len(os.Args) > 2 && os.Args[2] == "clean" {
			runReleaseClean(getDir(3))
		} else {
			dryRun := false
			dirIdx := 2
			for i := 2; i < len(os.Args); i++ {
				if os.Args[i] == "--dry-run" {
					dryRun = true
				}
			}
			// Find the first non-flag argument as dir.
			for i := 2; i < len(os.Args); i++ {
				if !strings.HasPrefix(os.Args[i], "-") {
					dirIdx = i
					break
				}
			}
			dir := getDir(dirIdx)
			runGen(dir)
			runRelease(dir, dryRun)
		}
	case "clean":
		dir := getDir(2)
		err := os.RemoveAll(filepath.Join(dir, ".inco_cache"))
		_ = err // @inco: err == nil, -panic(err)
		fmt.Println("inco: cache cleaned")
	default:
		fmt.Fprintf(os.Stderr, "inco: unknown command %q\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(1)
	}
}

// guardPanic recovers from panics (including those injected by @inco:)
// and exits cleanly with the panic message.
func guardPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "inco: %v\n", r)
		os.Exit(1)
	}
}

func getDir(argIdx int) string {
	// @inco: len(os.Args) <= argIdx, -return(os.Args[argIdx])
	return "."
}

func runGen(dir string) {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)
	err = inco.NewEngine(absDir).Run()
	_ = err // @inco: err == nil, -panic(err)
}

func runAudit(dir string) *inco.AuditResult {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)
	result, err := inco.Audit(absDir)
	_ = err // @inco: err == nil, -panic(err)
	return result
}

func runRelease(dir string, dryRun bool) {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)
	err = inco.Release(absDir, dryRun)
	_ = err // @inco: err == nil, -panic(err)
}

func runReleaseClean(dir string) {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)
	err = inco.ReleaseClean(absDir)
	_ = err // @inco: err == nil, -panic(err)
}

func runGo(subcmd, dir string, extraArgs []string) {
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
		execGo(subcmd, extraArgs)
		return
	}
	absOverlay, err := filepath.Abs(overlayPath)
	_ = err // @inco: err == nil, -panic(err)
	args := append([]string{fmt.Sprintf("-overlay=%s", absOverlay)}, extraArgs...)
	execGo(subcmd, args)
}

func execGo(subcmd string, args []string) {
	cmd := execCommand("go", append([]string{subcmd}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
