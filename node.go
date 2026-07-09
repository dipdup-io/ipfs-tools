package ipfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ipfs/boxo/files"
	boxopath "github.com/ipfs/boxo/path"
	icore "github.com/ipfs/kubo/core/coreiface"
	"github.com/ipfs/kubo/core/corerepo"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/ipfs/kubo/config"
	"github.com/ipfs/kubo/core"
	"github.com/ipfs/kubo/core/coreapi"
	"github.com/ipfs/kubo/plugin/loader" // This package is needed so that all the preloaded plugins are loaded automatically
	"github.com/ipfs/kubo/repo"
	"github.com/ipfs/kubo/repo/fsrepo"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Node -
type Node struct {
	api       icore.CoreAPI
	node      *core.IpfsNode
	providers []Provider
	limit     int64
	wg        sync.WaitGroup
}

// NewNode -
func NewNode(ctx context.Context, dir string, limit int64, blacklist []string, providers []Provider) (*Node, error) {
	api, node, err := spawn(ctx, dir, blacklist, providers)
	if err != nil {
		return nil, errors.Wrap(err, "failed to spawn node")
	}
	return &Node{
		api:       api,
		node:      node,
		providers: providers,
		limit:     limit,
	}, nil
}

// Start -
func (n *Node) Start(ctx context.Context) error {
	log.Info().Msg("going to connect to bootstrap nodes...")

	connected, err := n.api.Swarm().Peers(ctx)
	if err != nil {
		log.Warn().Msg("can't get perrs")
		return nil
	}
	for i := range connected {
		log.Info().
			Str("peer_id", connected[i].ID().String()).
			Str("address", connected[i].Address().String()).
			Msg("connected to peer")
	}

	n.wg.Go(func() {
		if err := corerepo.PeriodicGC(ctx, n.node); err != nil && !errors.Is(err, context.Canceled) {
			log.Err(err).Msg("ipfs periodic gc")
		}
	})

	return nil
}

// Close -
func (n *Node) Close() error {
	n.wg.Wait()
	return n.node.Close()
}

// Get -
func (n *Node) Get(ctx context.Context, cid string) (Data, error) {
	cidObj, err := boxopath.NewPath(cid)
	if err != nil {
		return Data{}, errors.Wrap(ErrInvalidCID, cid)
	}

	start := time.Now()
	rootNode, err := n.api.Unixfs().Get(ctx, cidObj)
	if err != nil {
		return Data{}, errors.Wrapf(err, "could not get file with CID: %s", cid)
	}
	defer rootNode.Close()
	responseTime := time.Since(start).Milliseconds()

	file := files.ToFile(rootNode)
	if file == nil {
		return Data{}, errors.Errorf("could not get file with CID: %s", cid)
	}

	data, err := io.ReadAll(io.LimitReader(file, n.limit))
	if err != nil {
		return Data{}, err
	}

	return Data{
		Raw:          data,
		Node:         "ipfs-metadata-node",
		ResponseTime: responseTime,
	}, nil
}

var loadPluginsOnce sync.Once

func spawn(ctx context.Context, dir string, blacklist []string, providers []Provider) (icore.CoreAPI, *core.IpfsNode, error) {
	var onceErr error
	loadPluginsOnce.Do(func() {
		onceErr = setupPlugins("")
	})
	if onceErr != nil {
		return nil, nil, onceErr
	}

	if err := createRepository(dir); err != nil {
		return nil, nil, err
	}

	r, err := openRepository(dir, blacklist, providers)
	if err != nil {
		return nil, nil, err
	}

	node, err := core.NewNode(ctx, &core.BuildCfg{
		Online: true,
		Repo:   r,
		ExtraOpts: map[string]bool{
			"enable-gc": true,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	api, err := coreapi.NewCoreAPI(node)
	return api, node, err
}

// openRepository opens an initialized repo and persists the current settings into its config.
func openRepository(dir string, blacklist []string, providers []Provider) (repo.Repo, error) {
	r, err := fsrepo.Open(dir)
	if err != nil {
		return nil, err
	}

	if err := updateRepoConfig(r, blacklist, providers); err != nil {
		if closeErr := r.Close(); closeErr != nil {
			log.Err(closeErr).Msg("closing ipfs repo")
		}
		return nil, err
	}

	return r, nil
}

func updateRepoConfig(r repo.Repo, blacklist []string, providers []Provider) error {
	current, err := r.Config()
	if err != nil {
		return err
	}

	updated, err := current.Clone()
	if err != nil {
		return err
	}
	if err := applySettings(updated, blacklist, providers); err != nil {
		return err
	}

	return r.SetConfig(updated)
}

func applySettings(cfg *config.Config, blacklist []string, providers []Provider) error {
	cfg.Swarm.DisableBandwidthMetrics = true
	cfg.Swarm.Transports.Network.Relay = config.False
	cfg.Swarm.Transports.Network.QUIC = config.False
	cfg.Swarm.AddrFilters = blacklist
	cfg.Swarm.ConnMgr.LowWater = config.NewOptionalInteger(20)
	cfg.Swarm.ConnMgr.HighWater = config.NewOptionalInteger(50)
	cfg.Swarm.ConnMgr.GracePeriod = config.NewOptionalDuration(time.Minute)

	cfg.Routing.AcceleratedDHTClient = config.False
	cfg.Routing.Type = config.NewOptionalString("delegated")
	cfg.Routing.DelegatedRouters = []string{"auto"}

	cfg.Provide.Enabled = config.False
	cfg.Bitswap.ServerEnabled = config.False

	cfg.Datastore.StorageMax = "2GB"
	cfg.Datastore.GCPeriod = "1h"

	peers, err := providersToAddrInfo(providers)
	if err != nil {
		return errors.Wrap(err, "collecting providers info")
	}
	cfg.Peering = config.Peering{Peers: peers}
	return nil
}

func createRepository(dir string) error {
	if fsrepo.IsInitialized(dir) {
		return nil
	}
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return errors.Wrap(err, "failed to create dir")
	}

	cfg, err := config.Init(io.Discard, 2048)
	if err != nil {
		return errors.Wrap(err, "config init")
	}

	return errors.Wrap(fsrepo.Init(dir, cfg), "failed to init node")
}

func setupPlugins(externalPluginsPath string) error {
	// Load any external plugins if available on externalPluginsPath
	plugins, err := loader.NewPluginLoader(filepath.Join(externalPluginsPath, "plugins"))
	if err != nil {
		return fmt.Errorf("error loading plugins: %s", err)
	}

	// Load preloaded and external plugins
	if err := plugins.Initialize(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	if err := plugins.Inject(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	return nil
}

func providersToAddrInfo(providers []Provider) ([]peer.AddrInfo, error) {
	peers := make([]peer.AddrInfo, 0)
	for i := range providers {
		id, err := peer.Decode(providers[i].ID)
		if err != nil {
			return nil, errors.Wrap(err, "providersToAddrInfo")
		}
		info := peer.AddrInfo{
			ID: id,
		}
		if providers[i].Address != "" {
			info.Addrs = []ma.Multiaddr{
				ma.StringCast(providers[i].Address),
			}
		}

		peers = append(peers, info)
	}
	return peers, nil
}
