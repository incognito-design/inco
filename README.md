# Inco

Inco is a compile-time assertion engine for Go. Write contract directives as plain comments; they are transformed into runtime guards in shadow files, wired in via `go build -overlay`.

## Philosophy

Business logic should be pure. Defensive noise — `if x == nil`, `if err != nil` — belongs in the shadow, not in your source.

Write the intent; Inco generates the shield.

### `if` is for logic, not for guarding

In an Inco codebase, `if` should express **logic flow** — branching on business conditions, selecting behavior. Not for:

- **Nil guards** → `// @inco: ptr != nil`
- **Value validation** → `// @inco: x > 0`
- **Range checks** → `// @inco: i < len(s)`

When every defensive check is a directive, the remaining `if` statements carry **real** semantic weight — genuine decisions, not boilerplate.

## Directive Syntax

Two forms — **standalone** and **inline**:

### Standalone (entire line is directive)

```
// @inco: <expr>
// @inco: <expr>, -panic("msg")
// @inco: <expr>, -return(values...)
// @inco: <expr>, -continue
// @inco: <expr>, -break
```

### Inline (code + trailing directive)

```go
_ = err // @inco: err == nil, -panic(err)
_ = skip // @inco: !skip, -return(filepath.SkipDir)
```

Inline directives attach to a code statement via `// @inco:` at the end of the line. The engine uses AST analysis to distinguish inline directives from decorative comments (e.g. struct field comments are ignored).

The default action is `-panic` with an auto-generated message.

### Examples

```go
func Transfer(from *Account, to *Account, amount int) {
    // @inco: from != nil
    // @inco: to != nil
    // @inco: amount > 0, -panic("amount must be positive")

    // ...
}
```

```go
func Parse(s string) (int, error) {
    // @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))
    return len(s), nil
}
```

```go
func (e *Engine) processFile(path string) {
    src, err := os.ReadFile(path)
    _ = err // @inco: err == nil, -panic(err)
    // ...
}
```

```go
func PrintPositive(nums []int) {
    for _, n := range nums {
        // @inco: n > 0, -continue
        fmt.Println(n)
    }
}
```

### Actions

| Action | Syntax | Meaning |
|--------|--------|---------|
| panic (default) | `// @inco: <expr>` | Panic with auto message |
| panic (custom) | `// @inco: <expr>, -panic("msg")` | Panic with custom message |
| return | `// @inco: <expr>, -return(vals...)` | Return specified values |
| return (bare) | `// @inco: <expr>, -return` | Bare return |
| continue | `// @inco: <expr>, -continue` | Continue enclosing loop |
| break | `// @inco: <expr>, -break` | Break enclosing loop |

### Generated Output

After `inco gen`, the above becomes a shadow file in `.inco_cache/`:

```go
func Transfer(from *Account, to *Account, amount int) {
    if !(from != nil) {
        panic("inco violation: from != nil (at transfer.go:2)")
    }
    if !(to != nil) {
        panic("inco violation: to != nil (at transfer.go:3)")
    }
    if !(amount > 0) {
        panic("amount must be positive")
    }

    // ...
}
```

For inline directives, the code line is preserved and the if-block is injected after it:

```go
func (e *Engine) processFile(path string) {
    src, err := os.ReadFile(path)
    _ = err
    if !(err == nil) {
        panic(err)
    }
    // ...
}
```

Shadow files live in `.inco_cache/` and are wired in via `go build -overlay`.

## Generics

Works with generic functions and types:

```go
func Clamp[N Number](val, lo, hi N) N {
    // @inco: lo <= hi
    if val < lo {
        return lo
    }
    if val > hi {
        return hi
    }
    return val
}

func NewPair[K comparable, V any](key K, value V) Pair[K, V] {
    // @inco: key != *new(K), -panic("key must not be zero")
    return Pair[K, V]{Key: key, Value: value}
}
```

## Auto-Import

When directive arguments reference standard library packages (e.g. `fmt.Sprintf`, `errors.New`), Inco automatically adds the corresponding import to the shadow file via `astutil.AddImport`. No manual import management needed.

## Usage

```bash
# Install
go install github.com/imnive-design/inco-go/cmd/inco@latest

# Generate overlay
inco gen [dir]

# Build / Test / Run with contracts enforced
inco build ./...
inco test ./...
inco run .

# Release: bake guards into source tree (no overlay needed)
inco release [dir]

# Revert release
inco release clean [dir]

# Contract coverage audit
inco audit [dir]

# Clean cache
inco clean [dir]
```

## Release Mode

`inco release` bakes guards into your source tree — no overlay, no build tags, no `inco` tool needed at build time.

### Convention: `.inco.go` files

Name source files that contain directives with a `.inco.go` extension:

```
foo.inco.go   ← source with @inco: directives
```

`inco gen` and `inco build` treat `.inco.go` files exactly like `.go` files (they end in `.go`, so the scanner picks them up).

### Release workflow

```bash
inco release .
```

For each `.inco.go` file in the overlay:

1. **Generate** `foo.go` — shadow content with guards injected (the `// Code generated by inco. DO NOT EDIT.` header is prepended)
2. **Backup** `foo.inco.go` → `foo.inco` — renamed so the Go compiler ignores it

After release:

```bash
go build ./...    # compiles foo.go (with guards) — no overlay, no inco needed
```

### Restore

```bash
inco release clean .
```

This removes each generated `foo.go` and restores `foo.inco` → `foo.inco.go`.

### When to use

- **Distribution**: ship a self-contained project with contracts baked in
- **CI/CD**: build with guards without installing `inco`
- **One-click restore**: `inco release clean` brings you back to development mode

## Build from Source

```bash
go install github.com/imnive-design/inco-go/cmd/inco@latest
```

```bash
make build      # inco build → bin/inco
make test       # Run tests with contracts enforced
make gen        # Regenerate overlay
make clean      # Remove .inco_cache/ and bin/
make install    # Install to $GOPATH/bin
```

## Audit

`inco audit` scans your codebase and reports:

- **@inco: coverage**: percentage of functions guarded by at least one `@inco:` directive
- **inco/(if+inco) ratio**: what fraction of all conditional guards are `@inco:` directives
- **Per-file breakdown**: directive and `if` counts per file
- **Unguarded functions**: list of functions without any `@inco:` directive
- **Ignored files**: files/dirs excluded by `.incoignore`

```
$ inco audit .
inco audit — contract coverage report
======================================

  Files scanned:  9
  Functions:      52

@inco: coverage:
  With @inco::     30 / 52  (57.7%)
  Without @inco::  22 / 52  (42.3%)

Directive vs if:
  @inco::           67
  ─────────────────────
  Total directives:   67
  Native if stmts:    59
  inco/(if+inco):     53.2%

Ignored by .incoignore (4):
  example/demo.inco.go
  example/edge_cases.inco.go
  example/generics.inco.go
  example/transfer.inco.go
```

The goal: drive `inco/(if+inco)` above 50%, meaning the majority of defensive checks live in directives rather than manual `if` statements.

## How It Works

1. `inco gen` scans all `.go` files for `// @inco:` comments (respecting `.incoignore`)
2. Uses `go/ast` to classify each directive as **standalone** (comment-only line) or **inline** (attached to a statement)
3. Generates shadow files in `.inco_cache/` — standalone directives become `if`-blocks in place; inline directives keep the code line and inject the `if`-block after it
4. Injects `//line` directives so panic stack traces point back to **original** source lines
5. Produces `overlay.json` for `go build -overlay`
6. Shadow files replace originals via overlay — source files are not modified on disk

### AST-Based Classification

The engine parses each source file as an AST and collects the set of line numbers that contain Go statements (`AssignStmt`, `ExprStmt`, `ReturnStmt`, etc.). When a `// @inco:` comment is found:

- **Comment-only line** → standalone directive (full line replaced by `if`-block)
- **Line in statement set** → inline directive (code preserved, `if`-block injected after)
- **Other** (struct field comment, etc.) → ignored

This prevents false matches on decorative comments like `RequireCount int // @inco: directives`.

## Project Structure

```
cmd/inco/           CLI: gen, build, test, run, audit, release, clean
internal/inco/      Core engine:
  audit.inco.go       Contract coverage auditing
  directive.inco.go   Directive parsing (@inco:)
  engine.inco.go      AST processing, code generation, overlay I/O
  ignore.inco.go      .incoignore file parsing and hierarchical matching
  release.inco.go     Release mode: bake guards into source
  types.inco.go       Core types (Directive, ActionKind, Overlay)
  walk.inco.go        Shared file traversal logic
example/            Demo files:
  demo.inco.go        @inco: basics
  transfer.inco.go    Multiple @inco: with panic
  edge_cases.inco.go  Closures, actions, edge cases
  generics.inco.go    Type parameters, generic containers
```

## .incoignore

Create a `.incoignore` file in any directory to exclude files from `inco gen` and `inco audit`. Patterns follow a simplified `.gitignore`-style syntax:

```
# Exclude example files from processing
example/*.inco.go

# Exclude a directory
generated/
```

Nested `.incoignore` files are supported — rules in a subdirectory apply only to that subtree. `inco audit` reports which files were ignored.

## Notes

Inco is self-hosting — it uses `@inco:` directives in its own source code. Since directives are plain Go comments, the code compiles with or without expansion.

### Inline directives solve the unused-variable problem

When a variable is only used in a directive, Go complains about an unused variable. The solution:

```go
_ = err // @inco: err == nil, -panic(err)
```

`_ = err` satisfies the compiler when building without inco, while `// @inco:` generates the real guard in the overlay.

## Design

- **Comment-based**: Plain Go comments — no custom syntax, no broken IDE support
- **Fail-fast**: panic by default — or return, continue, break as needed
- **Zero-overhead option**: Strip directives in production, or keep for fail-fast
- **Cache-friendly**: Content-hash (SHA-256) based shadow filenames for stable build cache
- **Source-mapped**: `//line` directives preserve original file:line in stack traces
- **Auto-import**: Standard library references in directive args are auto-imported

## License

MIT
