package main

import (
	"fmt"
	"os"
	"path/filepath"

	inco "github.com/incognito-design/inco/internal/inco"
)

const usage = `inco - Incognito Contract: invisible constraints, invincible code.

Usage:
  inco gen [dir]       Scan source files and generate overlay
  inco build [args]    Run gen + go build -overlay
  inco test [args]     Run gen + go test -overlay
  inco run [args]      Run gen + go run -overlay
  inco clean           Remove .inco_cache

If [dir] is omitted, the current directory is used.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]
	switch cmd {
	case "gen":
		dir := getDir(2)
		_ = runGen(dir) // @must
	case "build":
		dir := "."
		_ = runGen(dir) // @must
		runGo("build", dir, os.Args[2:])
	case "test":
		dir := "."
		_ = runGen(dir) // @must
		runGo("test", dir, os.Args[2:])
	case "run":
		dir := "."
		_ = runGen(dir) // @must
		runGo("run", dir, os.Args[2:])
	case "clean":
		dir := getDir(2)
		cache := filepath.Join(dir, ".inco_cache")
		_ = os.RemoveAll(cache) // @must
		fmt.Println("inco: cache cleaned")
	default:
		fmt.Fprintf(os.Stderr, "inco: unknown command %q\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}
}

func getDir(argIdx int) string {
	if len(os.Args) > argIdx {
		return os.Args[argIdx]
	}
	return "."
}

func runGen(dir string) error {
	// @require len(dir) > 0, "dir must not be empty"
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	engine := inco.NewEngine(absDir)
	return engine.Run()
}

func runGo(subcmd string, dir string, extraArgs []string) {
	// @require len(subcmd) > 0, "subcmd must not be empty"
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
		// No overlay generated, fallback to plain go command
		execGo(subcmd, extraArgs)
		return
	}
	absOverlay, _ := filepath.Abs(overlayPath) // @must
	args := append([]string{fmt.Sprintf("-overlay=%s", absOverlay)}, extraArgs...)
	execGo(subcmd, args)
}

func execGo(subcmd string, args []string) {
	// @require len(subcmd) > 0, "subcmd must not be empty"
	allArgs := append([]string{"go", subcmd}, args...)
	cmd := execCommand("go", append([]string{subcmd}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		_ = allArgs
		os.Exit(1)
	}
}
