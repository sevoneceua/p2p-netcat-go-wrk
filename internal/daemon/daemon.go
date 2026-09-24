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

// Daemon owns one p2p.Node and every tunnel started from one Config. The
// zero value is not usable; construct with Run.
type Daemon struct {
	Node *p2pnode.Node

	cancel context.CancelFunc
	wg     sync.WaitGroup
	log    Logger

	mu      sync.Mutex
	locks   []*listenerlock.Lock
	closers []io.Closer
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

	identityPath, err := expandHome(cfg.Identity)
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
	d := &Daemon{Node: node, cancel: cancel, log: log}

	for _, t := range cfg.Tunnels {
		if startErr := d.start(ctx, t); startErr != nil {
			_ = d.Close()
			return nil, fmt.Errorf("tunnel %q: %w", t.Name, startErr)
		}
	}
	return d, nil
}

// Close stops every tunnel, releases every logical-port lock, and closes
// the underlying Node. Safe to call once; a second call is a no-op beyond
// whatever the underlying Close calls themselves tolerate.
func (d *Daemon) Close() error {
	d.cancel()
	d.mu.Lock()
	closers := d.closers
	locks := d.locks
	d.closers = nil
	d.locks = nil
	d.mu.Unlock()

	for _, c := range closers {
		_ = c.Close()
	}
	for _, l := range locks {
		_ = l.Close()
	}
	err := d.Node.Close()
	d.wg.Wait()
	return err
}

func (d *Daemon) start(ctx context.Context, t tunnelconfig.Tunnel) error {
	token, err := loadTunnelToken(t)
	if err != nil {
		return err
	}
	if t.Mode == tunnelconfig.ModeServer {
		return d.startServer(ctx, t, token)
	}
	return d.startClient(ctx, t, token)
}

func (d *Daemon) addLock(l *listenerlock.Lock) {
	d.mu.Lock()
	d.locks = append(d.locks, l)
	d.mu.Unlock()
}

func (d *Daemon) addCloser(c io.Closer) {
	d.mu.Lock()
	d.closers = append(d.closers, c)
	d.mu.Unlock()
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
