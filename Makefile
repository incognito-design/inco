# Incognito Design // Inco Build Protocol

.PHONY: bootstrap gen build test run clean install

BOOTSTRAP_BIN := bin/inco-bootstrap
INCO_BIN      := bin/inco

# --- Stage 0: Bootstrap (plain go build, no contracts) ---
bootstrap:
	@echo "inco: stage 0 — building bootstrap binary (no contracts)..."
	@mkdir -p bin
	@go build -o $(BOOTSTRAP_BIN) ./cmd/inco
	@echo "inco: bootstrap binary ready at $(BOOTSTRAP_BIN)"

# --- Stage 1: Self-host (use bootstrap to build with contracts) ---

# Generate overlay from contract directives (using bootstrap)
gen: bootstrap
	@$(BOOTSTRAP_BIN) gen .

# Build with overlay (bootstrap generates overlay, then compiles with it)
build: bootstrap
	@$(BOOTSTRAP_BIN) gen .
	@$(BOOTSTRAP_BIN) build -o $(INCO_BIN) ./cmd/inco
	@echo "inco: self-hosted binary ready at $(INCO_BIN)"

# Test with overlay
test: bootstrap
	@$(BOOTSTRAP_BIN) test ./...

# Run with overlay
run: bootstrap
	@$(BOOTSTRAP_BIN) run .

# Clean cache and binaries
clean:
	@rm -rf .inco_cache bin/

# Install: bootstrap -> self-host -> install
install: bootstrap
	@$(BOOTSTRAP_BIN) gen .
	@go build -overlay .inco_cache/overlay.json -o $(GOPATH)/bin/inco ./cmd/inco 2>/dev/null || \
		go install ./cmd/inco
	@echo "inco: installed to GOPATH/bin"
