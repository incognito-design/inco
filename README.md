# Inco

Invisible constraints. Invincible code.

Inco is a Design-by-Contract (DbC) toolkit for Go. Define execution protocols using incognito comments; they are transformed into runtime assertions at build time via `-overlay`.

## Philosophy

Business logic should be pure. Defensive noise (`if x == nil`, `if err != nil`) belongs in the shadow, not in your source.

Write the intent; Inco generates the shield.

## Directives

```go
func Transfer(from *Account, to *Account, amount int) {
    // @require -nd from, to
    // @require amount > 0, "amount must be positive"
    
    res, _ := db.Exec(query) // @must
    
    // @ensure -nd res
}
```

| Directive | Meaning |
|-----------|---------|
| `// @require -nd var1, var2` | Precondition: variables must not be zero-valued |
| `// @require <expr>, "msg"` | Precondition: expression must be true |
| `// @ensure -nd var`         | Postcondition (via `defer`): must not be zero-valued at return |
| `// @must`                   | Execution assert: error must be nil, panic otherwise |

## Usage

```bash
# Install
go install github.com/incognito-design/inco/cmd/inco@latest

# Generate overlay
inco gen

# Build / Test / Run with contracts enforced
inco build ./...
inco test ./...
inco run .
```

## Build from source

Inco is self-bootstrapping — it uses its own contract directives in its source code.

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

## How it works

1. `inco gen` scans all `.go` files for `@require`, `@ensure`, `@must` comments
2. Generates shadow files with injected assertions in `.inco_cache/`
3. Produces `overlay.json` for `go build -overlay`
4. Source files remain untouched — zero invasion

## Project structure

```
cmd/inco/           CLI entry point
internal/inco/      Core engine (contract parsing, AST injection, overlay generation)
example/            Demo files showcasing all directive types
```

## Design

- **Zero-invasive**: No custom extensions, no broken IDE support
- **Zero-overhead option**: Strip via `-tags` in production, or keep for fail-fast
- **Self-bootstrapping**: Inco is built with Inco — its own source uses `@require` and `@must`
- **Cache-friendly**: Content-hash based filenames for stable build cache

## License

MIT
