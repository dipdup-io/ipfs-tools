# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Overview

Go library (single package `ipfs`, module `github.com/dipdup-io/ipfs-tools`) for fetching data from IPFS. No binaries — library only. Two independent entry points:

- **`Pool`** ([pool.go](pool.go)) — fetches content over HTTP from a list of IPFS gateways. Per-gateway rate limiting (10 rps via `golang.org/x/time/rate`), responses capped by a byte `limit` through `io.LimitReader`. Implements the `IPool` interface.
- **`Node`** ([node.go](node.go)) — embedded in-process Kubo node. `applySettings` deliberately minimizes resource usage (relay/QUIC off, delegated routing, providing and Bitswap server off, ConnMgr 20/50, 2GB storage, hourly GC) — keep that intent when touching the config.

Supporting files: [functions.go](functions.go) — link parsing/validation helpers (`Hash`, `Link`, `Path`, `FindAllLinks`, `Is`, `ShuffleGateways`); [data.go](data.go) — `Data` and `Provider` types; [errors.go](errors.go) — sentinel errors (`ErrInvalidCID`, `ErrNoIPFSResponse`, …).

## Commands

```bash
make test        # go test ./...
make lint        # golangci-lint run (v2 config in .golangci.yml)
go generate ./...  # regenerate mock_pool.go via mockgen (go.uber.org/mock)
```

Run a single test: `go test -run TestName ./...`

## Conventions

- Errors: wrap with `github.com/pkg/errors` (`errors.Wrap`/`Wrapf`); define sentinel errors in [errors.go](errors.go).
- Logging: `github.com/rs/zerolog/log` global logger.
- Mocks: `mock_pool.go` is generated — never edit by hand; change the `IPool` interface in [pool.go](pool.go) and run `go generate`.
- Linting is strict (gosec, gocritic, noctx, etc. — see [.golangci.yml](.golangci.yml)); always pass `context.Context` into HTTP requests.
- CI: GitHub Actions runs lint + tests on every push ([.github/workflows/test.yml](.github/workflows/test.yml)); tags `v*.*.*` trigger a GitHub release.

## Notes

- `Node.Get` expects a path accepted by `boxopath.NewPath` (e.g. `/ipfs/<cid>`), while `Pool` methods take `ipfs://<cid>` links — don't conflate the two.
- Kubo plugin loading in `spawn` uses `sync.Once`; only one plugin setup per process is possible.
