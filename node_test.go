package ipfs

import (
	"path/filepath"
	"testing"

	"github.com/ipfs/kubo/config"
	configserialize "github.com/ipfs/kubo/config/serialize"
	"github.com/ipfs/kubo/repo/fsrepo"
)

// well-known kubo bootstrap peer, used only as a syntactically valid fixture
const testProviderID = "QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN"

func loadTestPlugins(t *testing.T) {
	t.Helper()

	var err error
	loadPluginsOnce.Do(func() {
		err = setupPlugins("")
	})
	if err != nil {
		t.Fatalf("setupPlugins: %v", err)
	}
}

func readDiskConfig(t *testing.T, dir string) *config.Config {
	t.Helper()

	var cfg config.Config
	if err := configserialize.ReadConfigFile(filepath.Join(dir, "config"), &cfg); err != nil {
		t.Fatalf("reading repo config: %v", err)
	}
	return &cfg
}

func TestCreateRepositoryInitializesOnce(t *testing.T) {
	loadTestPlugins(t)
	dir := t.TempDir()

	if err := createRepository(dir); err != nil {
		t.Fatalf("createRepository: %v", err)
	}
	if !fsrepo.IsInitialized(dir) {
		t.Fatal("repo is not initialized after createRepository")
	}

	first := readDiskConfig(t, dir)
	if first.Identity.PeerID == "" {
		t.Fatal("fresh repo has empty PeerID")
	}

	if err := createRepository(dir); err != nil {
		t.Fatalf("createRepository on existing repo: %v", err)
	}

	second := readDiskConfig(t, dir)
	if first.Identity.PeerID != second.Identity.PeerID {
		t.Fatalf("identity was regenerated: %s != %s", first.Identity.PeerID, second.Identity.PeerID)
	}
}

func TestOpenRepositoryPersistsSettings(t *testing.T) {
	loadTestPlugins(t)
	dir := t.TempDir()

	if err := createRepository(dir); err != nil {
		t.Fatalf("createRepository: %v", err)
	}

	blacklist := []string{"/ip4/10.0.0.0/ipcidr/8"}
	providers := []Provider{{ID: testProviderID, Address: "/ip4/104.131.131.82/tcp/4001"}}

	r, err := openRepository(dir, blacklist, providers)
	if err != nil {
		t.Fatalf("openRepository: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repo close: %v", err)
	}

	cfg := readDiskConfig(t, dir)

	if got := cfg.Routing.Type.WithDefault(""); got != "delegated" {
		t.Errorf("Routing.Type = %q, want %q", got, "delegated")
	}
	if len(cfg.Routing.DelegatedRouters) != 1 || cfg.Routing.DelegatedRouters[0] != "auto" {
		t.Errorf("Routing.DelegatedRouters = %v, want [auto]", cfg.Routing.DelegatedRouters)
	}
	if cfg.Provide.Enabled.WithDefault(true) {
		t.Error("Provide.Enabled must be persisted as false")
	}
	if cfg.Bitswap.ServerEnabled.WithDefault(true) {
		t.Error("Bitswap.ServerEnabled must be persisted as false")
	}
	if cfg.Datastore.StorageMax != "2GB" {
		t.Errorf("Datastore.StorageMax = %q, want 2GB", cfg.Datastore.StorageMax)
	}
	if cfg.Datastore.GCPeriod != "1h" {
		t.Errorf("Datastore.GCPeriod = %q, want 1h", cfg.Datastore.GCPeriod)
	}
	if got := cfg.Swarm.ConnMgr.LowWater.WithDefault(0); got != 20 {
		t.Errorf("Swarm.ConnMgr.LowWater = %d, want 20", got)
	}
	if got := cfg.Swarm.ConnMgr.HighWater.WithDefault(0); got != 50 {
		t.Errorf("Swarm.ConnMgr.HighWater = %d, want 50", got)
	}
	if len(cfg.Swarm.AddrFilters) != 1 || cfg.Swarm.AddrFilters[0] != blacklist[0] {
		t.Errorf("Swarm.AddrFilters = %v, want %v", cfg.Swarm.AddrFilters, blacklist)
	}
	if len(cfg.Peering.Peers) != 1 || cfg.Peering.Peers[0].ID.String() != testProviderID {
		t.Errorf("Peering.Peers = %v, want single peer %s", cfg.Peering.Peers, testProviderID)
	}
}

func TestOpenRepositoryOverridesStaleConfig(t *testing.T) {
	loadTestPlugins(t)
	dir := t.TempDir()

	if err := createRepository(dir); err != nil {
		t.Fatalf("createRepository: %v", err)
	}
	before := readDiskConfig(t, dir)

	// simulate a repo left behind by an older ipfs-tools version
	r, err := fsrepo.Open(dir)
	if err != nil {
		t.Fatalf("fsrepo.Open: %v", err)
	}
	stale, err := r.Config()
	if err != nil {
		t.Fatalf("repo config: %v", err)
	}
	staleCopy, err := stale.Clone()
	if err != nil {
		t.Fatalf("config clone: %v", err)
	}
	staleCopy.Routing.Type = config.NewOptionalString("dhtclient")
	staleCopy.Swarm.ConnMgr.HighWater = config.NewOptionalInteger(150)
	if err := r.SetConfig(staleCopy); err != nil {
		t.Fatalf("seeding stale config: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repo close: %v", err)
	}

	r, err = openRepository(dir, nil, nil)
	if err != nil {
		t.Fatalf("openRepository: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repo close: %v", err)
	}

	after := readDiskConfig(t, dir)
	if got := after.Routing.Type.WithDefault(""); got != "delegated" {
		t.Errorf("stale Routing.Type was not overridden: got %q", got)
	}
	if got := after.Swarm.ConnMgr.HighWater.WithDefault(0); got != 50 {
		t.Errorf("stale Swarm.ConnMgr.HighWater was not overridden: got %d", got)
	}
	if after.Identity.PeerID != before.Identity.PeerID {
		t.Fatalf("identity changed on config update: %s != %s", before.Identity.PeerID, after.Identity.PeerID)
	}
}

func TestApplySettingsInvalidProvider(t *testing.T) {
	if err := applySettings(&config.Config{}, nil, []Provider{{ID: "not-a-peer-id"}}); err == nil {
		t.Fatal("expected error for invalid provider ID")
	}
}
