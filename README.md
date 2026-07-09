# ipfs-tools

[![Tests](https://github.com/dipdup-io/ipfs-tools/actions/workflows/test.yml/badge.svg)](https://github.com/dipdup-io/ipfs-tools/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dipdup-io/ipfs-tools.svg)](https://pkg.go.dev/github.com/dipdup-io/ipfs-tools)

Go library for fetching data from IPFS. It provides two ways to resolve content:

- **Gateway pool** — fetch documents over HTTP from a list of public/private IPFS gateways with per-gateway rate limiting;
- **Embedded node** — run an in-process [Kubo](https://github.com/ipfs/kubo) IPFS node tuned for low resource usage and fetch content directly from the network.

Plus a set of helpers for parsing and validating `ipfs://` links.

## Installation

```bash
go get github.com/dipdup-io/ipfs-tools
```

## Usage

### Gateway pool

`Pool` requests content from HTTP gateways. Each gateway gets its own rate limiter (10 requests per second). Response size is capped by `limit` (bytes).

```go
package main

import (
	"context"
	"fmt"

	ipfs "github.com/dipdup-io/ipfs-tools"
)

func main() {
	pool, err := ipfs.NewPool([]string{
		"https://ipfs.io",
		"https://cloudflare-ipfs.com",
	}, 1024*1024) // 1 MB response limit
	if err != nil {
		panic(err)
	}

	// tries gateways in random order until one responds
	data, err := pool.Get(context.Background(), "ipfs://QmY7Yh4UquoXHLPFo2XbhXkhBvFoPwmQUSa92pxnxjQuPU")
	if err != nil {
		panic(err)
	}

	fmt.Printf("received %d bytes from %s\n", len(data.Raw), data.Node)
}
```

Other ways to query the pool:

```go
// single request to one randomly chosen gateway
data, err := pool.GetFromRandomGateway(ctx, link)

// request to a specific gateway
data, err := pool.GetFromNode(ctx, link, "https://ipfs.io")
```

The pool implements the `IPool` interface; a `gomock` mock (`MockIPool`) is shipped with the package for testing.

### Embedded IPFS node

`Node` spawns an in-process Kubo node. The repo config is tuned to minimize resource usage: no relay/QUIC transports, delegated routing, providing and Bitswap server disabled, connection manager capped at 20–50 peers, 2 GB storage with hourly GC.

```go
node, err := ipfs.NewNode(ctx,
	"/path/to/ipfs-repo",       // repo directory (created if missing)
	1024*1024,                  // read limit in bytes
	[]string{},                 // swarm address blacklist (addr filters)
	[]ipfs.Provider{            // peers to keep persistent connections with
		{ID: "12D3KooW...", Address: "/dns4/example.com/tcp/4001"},
	},
)
if err != nil {
	panic(err)
}

if err := node.Start(ctx); err != nil { // connects to peers, starts periodic GC
	panic(err)
}
defer node.Close()

data, err := node.Get(ctx, "/ipfs/QmY7Yh4UquoXHLPFo2XbhXkhBvFoPwmQUSa92pxnxjQuPU")
```

### Helpers

```go
// extract and validate a CID from a link
hash, err := ipfs.Hash("ipfs://QmY7Yh4UquoXHLPFo2XbhXkhBvFoPwmQUSa92pxnxjQuPU")

// find all ipfs:// links (CIDv0 and CIDv1) in raw bytes
links := ipfs.FindAllLinks(data)

// check whether a string is a valid ipfs:// link
ok := ipfs.Is("ipfs://QmY7Yh4UquoXHLPFo2XbhXkhBvFoPwmQUSa92pxnxjQuPU")

// build a gateway URL from gateway address and hash
url := ipfs.Link("https://ipfs.io", hash) // https://ipfs.io/ipfs/<hash>

// strip the ipfs:// prefix
path := ipfs.Path("ipfs://<hash>") // <hash>
```

## Development

```bash
make test   # go test ./...
make lint   # golangci-lint run
```

Mocks are generated with [mockgen](https://github.com/uber-go/mock):

```bash
go generate ./...
```

## License

MIT — see [LICENSE](LICENSE).
