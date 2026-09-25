// Package daemon is the Epic-1 multi-tunnel supervisor: it takes one
// tunnelconfig.Config, builds a single p2p.Node from it, and starts every
// tunnel the config lists on that shared Node — the piece that turns the
// one-process-one-port CLI into an FRP-style "many tunnels, one config,
// one process" daemon.
//
// Every tunnel the daemon starts is treated as persistent (it accepts
// connections for as long as the daemon runs); the CLI's single-shot
// "accept one connection and exit" behavior has no equivalent here, since
// a daemon that exits after the first connection defeats the point.
package daemon

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/santaklouse/go-p2p-netcat/internal/appdir"
	"github.com/santaklouse/go-p2p-netcat/internal/identity"
	"github.com/santaklouse/go-p2p-netcat/internal/listenerlock"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	p2pnode "github.com/santaklouse/go-p2p-netcat/p2p"
)

// dialTimeout bounds every outbound dial the daemon makes on a tunnel's
// behalf: opening a p2p stream to a peer, and forwarding into a local
// TCP/UDP target on the server side.
const dialTimeout = 30 * time.Second

// authTimeout bounds the pairing-token handshake once a stream is open.
const authTimeout = 10 * time.Second

// Logger receives one line per notable daemon event (a tunnel starting,
// a session ending, an auth failure). A nil Logger passed to Run discards
// everything.
type Logger func(format string, args ...any)

// tunnelHandle is whatever a running tunnel needs released when it stops,
// either at full Close or when Reload drops/replaces it. Server tunnels
// set lock and proto; client tunnels set closer; a field left at its zero
// value is simply skipped during teardown.
type tunnelHandle struct {
	lock   *listenerlock.Lock // server tunnels: the logical-port lock
	proto  protocol.ID        // server tunnels: registered on Node.Host, to unregister
	closer io.Closer          // client tunnels: the local net.Listener/UDP listener
}

// Daemon owns one p2p.Node and every tunnel started from one Config. The
// zero value is not usable; construct with Run.
type Daemon struct {
	Node *p2pnode.Node

	cancel context.CancelFunc
	wg     sync.WaitGroup
	log    Logger

	mu      sync.Mutex
	ctx     context.Context
	cfg     *tunnelconfig.Config
	handles map[string]tunnelHandle
}

// Run validates cfg, loads or creates the node identity, builds one
// p2p.Node, and starts every tunnel in cfg.Tunnels on it. If any tunnel
// fails to start, Run tears down everything it already started (Node
// included) and returns that error — Run either starts the whole config
// or none of it, so a caller never has to guess which tunnels are live
// after a failed Run.
func Run(parent context.Context, cfg *tunnelconfig.Config, log Logger) (*Daemon, error) {
	if log == nil {
		log = func(string, ...any) {}
	}
	if cfg == nil {
		return nil, fmt.Errorf("tunnel config is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	identityPath, err := appdir.ResolvePath(cfg.Identity)
	if err != nil {
		return nil, fmt.Errorf("identity path: %w", err)
	}
	if identityPath == "" {
		identityPath = identity.DefaultPath()
	}
	privateKey, err := identity.LoadOrCreate(identityPath)
	if err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	node, err := p2pnode.New(parent, p2pnode.Config{
		PrivateKey:     privateKey,
		Relays:         relayAddrs(cfg.Relays),
		EnableDHT:      true,
		EnableMDNS:     true,
		EnablePubSub:   true,
		PubSubDiscover: cfg.FallbackEnabled(),
		EnableQUIC:     true,
		EnableWebRTC:   true,
		Listen:         true,
		UpstreamSOCKS:  cfg.UpstreamSOCKS,
	})
	if err != nil {
		return nil, fmt.Errorf("start p2p node: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)
	d := &Daemon{
		Node:    node,
		cancel:  cancel,
		log:     log,
		ctx:     ctx,
		cfg:     cfg,
		handles: make(map[string]tunnelHandle, len(cfg.Tunnels)),
	}

	for _, t := range cfg.Tunnels {
		handle, startErr := d.start(ctx, t)
		if startErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("tunnel %q: %w", t.Name, startErr)
		}
		d.handles[t.Name] = handle
	}
	return d, nil
}

// Reload diffs newCfg against the config the Daemon is currently running:
// tunnels whose name disappears, or whose definition changed, are stopped;
// tunnels that are new, or whose definition changed, are (re)started.
// Unchanged tunnels (by name and by every field) are left running
// untouched — a reload never interrupts a tunnel nothing actually changed
// about, so unrelated in-flight sessions on other tunnels are unaffected.
//
// The identity, relays, upstream_socks and fallback_to_public_dht fields
// are read once at Run and are NOT re-applied by Reload: changing them
// requires restarting the whole daemon (they belong to the one shared
// Node, not to an individual tunnel, so hot-swapping them would mean
// rebuilding the Host underneath every tunnel at once). If newCfg differs
// from the running config in any of those fields, Reload still applies
// the tunnel-level diff and returns no error, but the node-level fields
// keep their original values — callers that care can compare Config()
// themselves and warn the user.
//
// On a tunnel start failure mid-reload, Reload stops, leaves already-
// applied changes from this call in place (they are not rolled back),
// and returns the error — the daemon keeps running rather than tearing
// itself down over a bad reload, which matters most exactly when the
// failure is a typo in the file that triggered the reload.
func (d *Daemon) Reload(newCfg *tunnelconfig.Config) error {
	if newCfg == nil {
		return fmt.Errorf("tunnel config is required")
	}
	if err := newCfg.Validate(); err != nil {
		return err
	}

	d.mu.Lock()
	oldCfg := d.cfg
	d.mu.Unlock()

	oldByName := tunnelsByName(oldCfg.Tunnels)
	newByName := tunnelsByName(newCfg.Tunnels)

	for name, oldTunnel := range oldByName {
		newTunnel, stillPresent := newByName[name]
		if !stillPresent || newTunnel != oldTunnel {
			d.stop(name)
		}
	}
	for name, newTunnel := range newByName {
		oldTunnel, existedBefore := oldByName[name]
		if existedBefore && oldTunnel == newTunnel {
			continue
		}
		handle, err := d.start(d.ctx, newTunnel)
		if err != nil {
			return fmt.Errorf("reload: tunnel %q: %w", name, err)
		}
		d.mu.Lock()
		d.handles[name] = handle
		d.mu.Unlock()
	}

	d.mu.Lock()
	d.cfg = newCfg
	d.mu.Unlock()
	return nil
}

// Config returns the config the Daemon is currently running (the most
// recent one Run or Reload accepted). Safe to call concurrently.
func (d *Daemon) Config() *tunnelconfig.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg
}

// Close stops every tunnel, releases every logical-port lock, and closes
// the underlying Node. Safe to call once; a second call is a no-op beyond
// whatever the underlying Close calls themselves tolerate.
func (d *Daemon) Close() error {
	d.cancel()
	d.mu.Lock()
	names := make([]string, 0, len(d.handles))
	for name := range d.handles {
		names = append(names, name)
	}
	d.mu.Unlock()
	for _, name := range names {
		d.stop(name)
	}
	err := d.Node.Close()
	d.wg.Wait()
	return err
}

// stop releases one tunnel's resources and removes it from d.handles. It
// does not interrupt sessions already in flight on that tunnel (those are
// tracked by the shared d.wg and simply run to completion); it only stops
// the tunnel from accepting new work.
func (d *Daemon) stop(name string) {
	d.mu.Lock()
	handle, ok := d.handles[name]
	delete(d.handles, name)
	d.mu.Unlock()
	if !ok {
		return
	}
	if handle.proto != "" {
		d.Node.Host.RemoveStreamHandler(handle.proto)
	}
	if handle.closer != nil {
		_ = handle.closer.Close()
	}
	if handle.lock != nil {
		_ = handle.lock.Close()
	}
}

func (d *Daemon) start(ctx context.Context, t tunnelconfig.Tunnel) (tunnelHandle, error) {
	token, err := loadTunnelToken(t)
	if err != nil {
		return tunnelHandle{}, err
	}
	if t.Mode == tunnelconfig.ModeServer {
		return d.startServer(ctx, t, token)
	}
	return d.startClient(ctx, t, token)
}

func tunnelsByName(tunnels []tunnelconfig.Tunnel) map[string]tunnelconfig.Tunnel {
	byName := make(map[string]tunnelconfig.Tunnel, len(tunnels))
	for _, t := range tunnels {
		byName[t.Name] = t
	}
	return byName
}

// relayAddrs sorts cfg.Relays by ascending Priority (lower runs first,
// matching tunnelconfig.Relay's doc comment) and returns just the
// multiaddr strings p2pnode.Config.Relays expects. Stable so relays
// sharing a priority keep their config-file order.
func relayAddrs(relays []tunnelconfig.Relay) []string {
	sorted := make([]tunnelconfig.Relay, len(relays))
	copy(sorted, relays)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Priority < sorted[j-1].Priority; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	addrs := make([]string, len(sorted))
	for i, r := range sorted {
		addrs[i] = r.Addr
	}
	return addrs
}
