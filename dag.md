---
name: gfy
description: Build/test/lint targets for gfy as a bashy dag pipeline (agent-first; the Makefile is generated from this file and only forwards here)
type: dag
---

# gfy — DAG task file

The Makefile in this directory is GENERATED from this file (`bashy dag --makefile`)
and only forwards to the targets below — do not edit it. This DAG file is the
agent-first task list, runnable with `bashy dag`:

```bash
bashy dag --list            # available targets
bashy dag build             # build the gfy binary (version via ldflags)
bashy dag test              # run all tests
bashy dag --json test       # machine-readable envelope for an agent
```

The version baked into the binary comes from the `VERSION` environment variable
(or a dag override, `bashy dag build VERSION=v1.2.3`), else `git describe`, else
`dev` — the old Makefile's rule.

## Tasks

### build
Build the gfy binary into ./gfy, injecting the version via ldflags.
Sources: cmd/, pkg/, internal/, go.mod, go.sum
Generates: gfy
Effects: exec,net,write

```bash
set -e
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
go build -ldflags "-X main.version=$version" -o gfy ./cmd/gfy
```

### tidy
Run go mod tidy, go fmt, and go vet over cmd, pkg and internal.
Effects: exec,net,write

```bash
set -e
go mod tidy
go fmt ./cmd/... ./pkg/... ./internal/...
go vet ./cmd/... ./pkg/... ./internal/...
```

### test
Run all tests. Scoped to ./cmd/... ./pkg/... ./internal/... as the old
Makefile ran them.
Effects: exec,net,write

```bash
go test ./cmd/... ./pkg/... ./internal/...
```

### lint
Run golangci-lint.
Effects: exec,net,write

```bash
golangci-lint run
```

### install
Install gfy to $GOPATH/bin with the version ldflags.
Effects: exec,net,write

```bash
set -e
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
go install -ldflags "-X main.version=$version" ./cmd/gfy
```

### diff
Compare the local working tree against the remote tracking branch.
Requires: build
Effects: exec,net,write

```bash
./gfy diff .
```

### clean
Remove build artifacts (the gfy binary and .gfy-out).
Effects: destroy

```bash
rm -f gfy
rm -rf .gfy-out
```

### fmtcheck
The gofmt gate (reports unformatted files, never rewrites them), plus a check
that the generated Makefile still matches this file — regenerate with
`bashy dag --makefile` instead of editing it.
Effects: exec,read

```bash
set -e
./scripts/fmtcheck.sh
bashy dag --makefile --check
```

### hooks
Install the pre-push formatting gate.
Effects: exec,write

```bash
git config core.hooksPath scripts/hooks
echo "hooks installed: core.hooksPath=scripts/hooks"
```
