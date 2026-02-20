package main

import (
	"fmt"
	"os"
	"path/filepath"

	inco "github.com/incognito-design/inco/internal/inco"
)

const usage = `inco — invisible constraints, invincible code.

Usage:
  inco gen [dir]           Scan source files and generate overlay
  inco build [args]        Run gen + go build -overlay
  inco test [args]         Run gen + go test -overlay
  inco run [args]          Run gen + go run -overlay
  inco audit [dir]         Contract coverage report
  inco release [dir]       Copy guards into source tree with //go:build inco
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
			dir := getDir(2)
			runGen(dir)
			runRelease(dir)
		}
	case "clean":
		dir := getDir(2)
		_ = os.RemoveAll(filepath.Join(dir, ".inco_cache")) // @must
		fmt.Println("inco: cache cleaned")
	default:
		fmt.Fprintf(os.Stderr, "inco: unknown command %q\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(1)
	}
}

// guardPanic recovers from panics (including those injected by @must)
// and exits cleanly with the panic message.
func guardPanic() {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "inco: %v\n", r)
		os.Exit(1)
	}
}

func getDir(argIdx int) string {
	if len(os.Args) > argIdx {
		return os.Args[argIdx]
	}
	return "."
}

func runGen(dir string) {
	absDir, _ := filepath.Abs(dir) // @must
	inco.NewEngine(absDir).Run()
}

func runAudit(dir string) *inco.AuditResult {
	absDir, _ := filepath.Abs(dir) // @must
	return inco.Audit(absDir)
}

func runRelease(dir string) {
	absDir, _ := filepath.Abs(dir) // @must
	inco.Release(absDir)
}

func runReleaseClean(dir string) {
	absDir, _ := filepath.Abs(dir) // @must
	inco.ReleaseClean(absDir)
}

func runGo(subcmd, dir string, extraArgs []string) {
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
		execGo(subcmd, extraArgs)
		return
	}
	absOverlay, _ := filepath.Abs(overlayPath) // @must
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
