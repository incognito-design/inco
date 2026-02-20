# Inco

Invisible constraints. Invincible code.

Inco is a compile-time assertion engine for Go. Write contract directives as plain comments; they are transformed into `panic`-based runtime guards in shadow files, wired in via `go build -overlay`. Your source stays untouched.

## Philosophy

Business logic should be pure. Defensive noise — `if x == nil`, `if err != nil` — belongs in the shadow, not in your source.

Write the intent; Inco generates the shield.

### `if` is for logic, not for guarding

In an Inco codebase, `if` should express **logic flow** — branching on business conditions, selecting behavior. Not for:

- **Nil guards** → `// @require ptr != nil`
- **Value validation** → `// @require x > 0`
- **Error checks** → `// @must`
- **Boolean checks** → `// @expect`
- **Postconditions** → `// @ensure`

When every defensive check is a directive, the remaining `if` statements carry **real** semantic weight — genuine decisions, not boilerplate.

## Directives

Four directive types, one action: **panic**.

```go
func Transfer(from *Account, to *Account, amount int) {
    // @require from != nil
    // @require to != nil
    // @require amount > 0 panic("amount must be positive")

    res, _ := db.Exec(query) // @must

    v, _ := cache[key] // @expect panic("key not found: " + key)
}
```

```go
func Abs(x int) int {
    // @ensure result >= 0
    result := x
    if x < 0 {
        result = -x
    }
    return result
}
```

| Directive | Position | Meaning |
|-----------|----------|---------|
| `// @require <expr>` | standalone | Precondition: expression must be true, else panic |
| `// @require <expr> panic("msg")` | standalone | Precondition with custom panic message |
| `// @must` | inline | Error check: captured `error` must be nil, else panic |
| `// @must panic("msg")` | inline | Error check with custom panic message |
| `// @expect` | inline | Bool check: captured `bool` must be true, else panic |
| `// @expect panic("msg")` | inline | Bool check with custom panic message |
| `// @ensure <expr>` | standalone | Postcondition: checked via `defer` at function exit |
| `// @ensure <expr> panic("msg")` | standalone | Postcondition with custom panic message |

### Generated Output

After `inco gen`, the above becomes a shadow file in `.inco_cache/`:

```go
func Transfer(from *Account, to *Account, amount int) {
    if !(from != nil) {
        panic("require violation: from != nil (at transfer.go:2)")
    }
    if !(to != nil) {
        panic("require violation: to != nil (at transfer.go:3)")
    }
    if !(amount > 0) {
        panic("amount must be positive")
    }

    res, __inco_err := db.Exec(query)
    if __inco_err != nil {
        panic(__inco_err)
    }

    v, __inco_ok := cache[key]
    if !__inco_ok {
        panic("key not found: " + key)
    }
}
```

Source files remain untouched. Shadow files live in `.inco_cache/` and are wired in via `go build -overlay`.

## `@require` — Preconditions

Standalone comment lines. The expression is a Go boolean expression; if it evaluates to false at runtime, the program panics.

```go
func CreateUser(name string, age int) {
    // @require len(name) > 0
    // @require age > 0
    // ...
}

func GetUser(u *User) {
    // @require u != nil panic("user must not be nil")
    // ...
}
```

The `panic` keyword is unambiguous: since `panic` is a Go builtin, it cannot appear as a standalone identifier in a valid boolean expression. Everything before the trailing `panic` is the expression; `panic("msg")` supplies the custom message.

## `@must` — Error Assertions

Inline on a line containing an `error`-returning call. Replaces the last `_` with a generated variable, then asserts it is nil.

```go
user, _ := db.Query("SELECT ...") // @must
```

Generated:

```go
user, __inco_err := db.Query("SELECT ...")
if __inco_err != nil {
    panic(__inco_err)
}
```

With a custom message (use `_` to reference the captured error):

```go
user, _ := db.Query("SELECT ...") // @must panic(fmt.Sprintf("query failed: %v", _))
```

Generated:

```go
user, __inco_err := db.Query("SELECT ...")
if __inco_err != nil {
    panic(fmt.Sprintf("query failed: %v", __inco_err))
}
```

## `@expect` — Boolean Assertions

Inline on a line with a comma-ok pattern. Replaces the last `_` with a generated bool variable, then asserts it is true.

```go
v, _ := m[key] // @expect panic("key not found: " + key)
```

Generated:

```go
v, __inco_ok := m[key]
if !__inco_ok {
    panic("key not found: " + key)
}
```

## `@ensure` — Postconditions (Design by Contract)

Standalone comment, same syntax as `@require`, but wraps the check in `defer` so it runs when the function returns. Use it to assert invariants on return values.

```go
func Abs(x int) int {
    // @ensure result >= 0
    result := x
    if x < 0 {
        result = -x
    }
    return result
}
```

Generated:

```go
func Abs(x int) int {
    defer func() {
        if !(result >= 0) {
            panic("ensure violation: result >= 0 (at abs.go:2)")
        }
    }()
    result := x
    if x < 0 {
        result = -x
    }
    return result
}
```

With `@require` + `@ensure` together (full DbC):

```go
func SafeSlice(s []int, start, end int) []int {
    // @require start >= 0
    // @require end <= len(s)
    // @ensure len(result) == end-start
    result := s[start:end]
    return result
}
```

## Generics

Works with generic functions and types:

```go
func Clamp[N Number](val, lo, hi N) N {
    // @require lo <= hi
    if val < lo {
        return lo
    }
    if val > hi {
        return hi
    }
    return val
}

type Repository[T any] struct {
    data map[string]T
}

func (r *Repository[T]) Get(id string) T {
    v, _ := r.data[id] // @expect panic("not found: " + id)
    return v
}

func NewPair[K comparable, V any](key K, value V) Pair[K, V] {
    // @require key != *new(K) panic("key must not be zero")
    return Pair[K, V]{Key: key, Value: value}
}
```

## Auto-Import

When directive arguments reference standard library packages (e.g. `fmt.Sprintf`, `errors.New`), Inco automatically adds the corresponding import to the shadow file via `astutil.AddImport`. No manual import management needed.

## Usage

```bash
# Install
go install github.com/incognito-design/inco/cmd/inco@latest

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

`inco release` copies the generated guard files from `.inco_cache/` directly
into your source tree, using `//go:build` tags for isolation:

```bash
inco release .
```

This does two things for each guarded file:

1. **Original** `foo.go` — prepends `//go:build !inco` (excluded when tag is active)
2. **Guard copy** `foo_inco.go` — shadow content with `//go:build inco` (included when tag is active)

After release, build with or without contracts — no `-overlay` required:

```bash
go build -tags inco ./...    # with contracts (uses foo_inco.go)
go build ./...               # without contracts (uses foo.go)
```

This is useful for:
- **Distribution**: ship a self-contained project with optional contracts
- **CI/CD**: switch contracts on/off via build tags without needing the `inco` tool at build time

To revert and go back to overlay-based development:

```bash
inco release clean .
```

This removes all `_inco.go` files and strips the `//go:build !inco` tags from originals.

## Build from Source

```bash
# Two-stage build:
make build
# Stage 0: plain go build → bin/inco-bootstrap (no contracts)
# Stage 1: bootstrap generates overlay → compiles with contracts → bin/inco

# Other targets:
make test       # Run tests with contracts enforced
make clean      # Remove .inco_cache/ and bin/
make install    # Install to $GOPATH/bin
```

## Audit

`inco audit` scans your codebase and reports:

- **@require coverage**: percentage of functions guarded by at least one `@require`
- **Directive vs if ratio**: total `@require` / `@must` / `@expect` / `@ensure` directives compared to native `if` statements
- **Per-file breakdown**: directive and `if` counts per file
- **Unguarded functions**: list of functions without any `@require`

```
$ inco audit .
inco audit — contract coverage report
======================================

  Files scanned:  10
  Functions:      62

@require coverage:
  With @require:     10 / 62  (16.1%)
  Without @require:  52 / 62  (83.9%)

Directive vs if:
  @require:           18
  @must:              4
  @expect:            3
  @ensure:            2
  ─────────────────────
  Total directives:   25
  Native if stmts:    122
  Directive/if ratio: 0.20

Per-file breakdown:
  File                        @require  @must  @expect  @ensure  if  funcs  guarded
  ──────────────────────────  ────────  ─────  ───────  ──  ─────  ───────
  example/demo.go                   5      1        0   0      4        3
  example/edge_cases.go             6      1        1   0      5        3
  internal/inco/engine.go           0      0        0  45     19        0
  ...

Functions without @require (46):
  cmd/inco/main.go:24  main
  internal/inco/engine.go:52  Engine.Run
  ...
```

The goal: drive `@require` coverage up and the directive/if ratio toward 1.0+, meaning most defensive checks live in directives rather than manual `if` statements.

## How It Works

1. `inco gen` scans all `.go` files for `@require`, `@must`, `@expect`, `@ensure` comments
2. Parses directives and generates shadow files with injected `if`/`panic`/`defer` blocks in `.inco_cache/`
3. Injects `//line` directives so panic stack traces point back to **original** source lines
4. Produces `overlay.json` for `go build -overlay`
5. Source files remain untouched — zero invasion

## Project Structure

```
cmd/inco/           CLI: gen, build, test, run, audit, release, clean
internal/inco/      Core engine:
  audit.go            Contract coverage auditing
  directive.go        Directive parsing (@require, @must, @expect, @ensure)
  engine.go           AST processing, code generation, overlay I/O
  release.go          Release mode: bake guards into source with build tags
example/            Demo files:
  demo.go             @require + @must basics
  transfer.go         Multiple @require, @must
  edge_cases.go       Closures, custom panic, @expect, @ensure
  generics.go         Type parameters, generic containers
```

## Design

- **Zero-invasive**: Plain Go comments — no custom syntax, no broken IDE support
- **One action**: `panic` only — fail-fast by design
- **Zero-overhead option**: Strip directives in production, or keep for fail-fast
- **Cache-friendly**: Content-hash (SHA-256) based shadow filenames for stable build cache
- **Source-mapped**: `//line` directives preserve original file:line in stack traces
- **Auto-import**: Standard library references in directive args are auto-imported

## License

MIT
