// Package tunnelconfig defines the YAML configuration schema used by the
// p2p-nc multi-tunnel daemon (Epic 1). A single Config describes one
// libp2p.Host ("node") and any number of tunnels that share it, each
// tunnel corresponding to one logical port already used by the p2p-nc
// protocol (see p2p.ProtocolForService).
//
// The schema intentionally mirrors the flags already understood by
// internal/cli (-l, -p, -d, -S, -e, -i, -u), but gives every role an
// explicit name instead of overloading -p/-d across server and client
// modes, so a config reads correctly without cross-referencing the CLI.
package tunnelconfig

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	// v2, not v3: v2 is already an indirect dependency of this module
	// (pulled in transitively), so using it here adds zero new modules
	// to fetch/verify. Its API (Unmarshal + `yaml:"..."` tags) is
	// identical to v3 for our purposes.
	yaml "go.yaml.in/yaml/v2"
)

// TunnelType selects which session the tunnel runs, mirroring the
// mutually-exclusive CLI modes in internal/cli.privilegedListenerMode.
type TunnelType string

const (
	// TunnelForward pipes raw bytes between a local TCP/UDP endpoint and
	// the p2p stream. Valid in both server and client Mode.
	TunnelForward TunnelType = "forward"
	// TunnelSOCKS runs session.SOCKS on the remote peer: the server acts
	// as a SOCKS4/4a/5 exit node. Server-only, matching the CLI's -S,
	// which internal/cli/root.go accepts only with -l.
	TunnelSOCKS TunnelType = "socks"
	// TunnelPTY runs an interactive remote shell (session.PTYServer).
	// Server-only; there is no unattended client-side use for an
	// interactive PTY, so it is intentionally left out of Mode "client".
	TunnelPTY TunnelType = "pty"
	// TunnelExec runs a fixed command per connection (session.Exec).
	// Server-only, matching the CLI's -e, which is rejected without -l.
	TunnelExec TunnelType = "exec"
)

// Mode selects which side of the tunnel this process plays.
type Mode string

const (
	ModeServer Mode = "server"
	ModeClient Mode = "client"
)

// Protocol selects the local transport a Forward tunnel carries.
type Protocol string

const (
	ProtocolTCP Protocol = "tcp"
	ProtocolUDP Protocol = "udp"
)

// Relay is one candidate relay multiaddr, ordered by Priority (lower runs
// first). It maps onto p2p.Config.Relays, which today takes a flat
// []string; Priority lets the daemon try them in order before falling
// back to opportunistic public discovery.
type Relay struct {
	Addr     string `yaml:"addr"`
	Priority int    `yaml:"priority,omitempty"`
}

// Config is the top-level daemon configuration: one node (one libp2p
// identity/Host) plus the tunnels it serves or dials.
type Config struct {
	// Identity is the path to the node's private key file. Empty means
	// identity.DefaultPath(), same fallback as the single-shot CLI.
	Identity string `yaml:"identity,omitempty"`

	Relays []Relay `yaml:"relays,omitempty"`

	// FallbackToPublicDHT keeps opportunistic public relay discovery
	// (EnableAutoRelayWithPeerSource) running alongside any static
	// Relays, instead of the CLI's current either/or behavior. A nil
	// value defaults to true — see Config.fallbackToPublicDHT().
	FallbackToPublicDHT *bool `yaml:"fallback_to_public_dht,omitempty"`

	// UpstreamSOCKS routes the node's own outbound transport dials
	// (relay and peer connections) through an external SOCKS proxy
	// (host:port, SOCKS5, no auth). This is a node-level setting — one
	// Host has one underlying dialer, so it cannot vary per tunnel.
	UpstreamSOCKS string `yaml:"upstream_socks,omitempty"`

	Tunnels []Tunnel `yaml:"tunnels"`
}

// Tunnel is one forwarded service sharing the daemon's Host.
type Tunnel struct {
	// Name must be unique within the Config; it is used for logging and
	// for hot-reload diffing (Epic 1, later step), never for routing.
	Name string `yaml:"name"`

	Type TunnelType `yaml:"type"`
	Mode Mode       `yaml:"mode"`

	// LogicalPort is the p2p-nc protocol service id (p2p.ProtocolForService).
	// It is the tunnel's address on the p2p network, unrelated to any
	// local TCP/UDP port below. Required for every tunnel type: server
	// tunnels listen on it, client tunnels dial it on Peer.
	LogicalPort uint16 `yaml:"logical_port"`

	// --- forward-only fields ---

	// Protocol selects TCP or UDP for Type Forward. Required when
	// Type == TunnelForward, ignored otherwise.
	Protocol Protocol `yaml:"protocol,omitempty"`

	// Target is "host:port" the server dials locally once a peer
	// connects (mirrors -d/-p on the CLI's listener side). Required
	// when Type == TunnelForward && Mode == ModeServer. Host defaults
	// to 127.0.0.1 if omitted (e.g. ":5432").
	Target string `yaml:"target,omitempty"`

	// Listen is "bind:port" the client listens on locally; every
	// accepted connection is forwarded into the tunnel (mirrors -p/-b
	// on the CLI's client side). Required when Type == TunnelForward
	// && Mode == ModeClient.
	Listen string `yaml:"listen,omitempty"`

	// --- client-only fields ---

	// Peer is the remote PeerId to dial. Required when Mode == ModeClient.
	Peer string `yaml:"peer,omitempty"`

	// --- exec-only fields ---

	// Exec is the command line run per connection. Required when
	// Type == TunnelExec.
	Exec string `yaml:"exec,omitempty"`

	// --- auth ---

	// TokenFile is a pairing-token file (see protocol/pairing). Either
	// this or AllowUnauthenticated must be set for a server tunnel of
	// a privileged Type (everything but a plain Forward with no
	// authentication is intentionally out of scope for v1 — see
	// Validate).
	TokenFile string `yaml:"token_file,omitempty"`

	// AllowUnauthenticated opts out of the token requirement, mirroring
	// --allow-unauthenticated-listener. Off by default.
	AllowUnauthenticated bool `yaml:"allow_unauthenticated,omitempty"`
}

// Parse decodes YAML bytes into a Config and validates it.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse tunnel config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// fallbackToPublicDHT resolves the FallbackToPublicDHT default (true).
func (c *Config) FallbackEnabled() bool {
	return c.FallbackToPublicDHT == nil || *c.FallbackToPublicDHT
}

// Validate checks the config for internal consistency: it does not touch
// the network, the filesystem (beyond TokenFile existing is NOT checked
// here — that happens at load time in the daemon, where a missing file
// should fail that one tunnel, not the whole config), or libp2p.
func (c *Config) Validate() error {
	if len(c.Tunnels) == 0 {
		return fmt.Errorf("tunnel config: at least one tunnel is required")
	}
	if c.UpstreamSOCKS != "" {
		if _, _, err := net.SplitHostPort(c.UpstreamSOCKS); err != nil {
			return fmt.Errorf("upstream_socks: %w", err)
		}
	}
	for i, relay := range c.Relays {
		if strings.TrimSpace(relay.Addr) == "" {
			return fmt.Errorf("relays[%d]: addr is required", i)
		}
	}

	names := make(map[string]int, len(c.Tunnels))
	serverPorts := make(map[uint16]string, len(c.Tunnels))
	for i, t := range c.Tunnels {
		if err := t.validate(); err != nil {
			return fmt.Errorf("tunnels[%d] (%s): %w", i, orIndex(t.Name, i), err)
		}
		if t.Name == "" {
			return fmt.Errorf("tunnels[%d]: name is required", i)
		}
		if prev, exists := names[t.Name]; exists {
			return fmt.Errorf("tunnels[%d] (%s): name duplicates tunnels[%d]", i, t.Name, prev)
		}
		names[t.Name] = i

		// listenerlock.Acquire locks purely by numeric logical port,
		// independent of protocol/type, so two server tunnels can never
		// share one — see internal/listenerlock/lock.go.
		if t.Mode == ModeServer {
			if prev, exists := serverPorts[t.LogicalPort]; exists {
				return fmt.Errorf("tunnels[%d] (%s): logical_port %d already used by server tunnel %q",
					i, t.Name, t.LogicalPort, prev)
			}
			serverPorts[t.LogicalPort] = t.Name
		}
	}
	return nil
}

func (t *Tunnel) validate() error {
	switch t.Type {
	case TunnelForward, TunnelSOCKS, TunnelPTY, TunnelExec:
	case "":
		return fmt.Errorf("type is required (forward, socks, pty, or exec)")
	default:
		return fmt.Errorf("unknown type %q", t.Type)
	}
	switch t.Mode {
	case ModeServer, ModeClient:
	case "":
		return fmt.Errorf("mode is required (server or client)")
	default:
		return fmt.Errorf("unknown mode %q", t.Mode)
	}
	if t.LogicalPort == 0 {
		return fmt.Errorf("logical_port must be between 1 and 65535")
	}

	// Server-only types, matching internal/cli/root.go's
	// privilegedListenerMode/validateOptions combinations.
	if t.Mode == ModeClient {
		switch t.Type {
		case TunnelSOCKS, TunnelPTY, TunnelExec:
			return fmt.Errorf("type %q is server-only", t.Type)
		}
		if strings.TrimSpace(t.Peer) == "" {
			return fmt.Errorf("peer is required in client mode")
		}
	}

	switch t.Type {
	case TunnelForward:
		if t.Protocol != ProtocolTCP && t.Protocol != ProtocolUDP {
			return fmt.Errorf("protocol must be %q or %q for a forward tunnel", ProtocolTCP, ProtocolUDP)
		}
		if t.Mode == ModeServer {
			if strings.TrimSpace(t.Target) == "" {
				return fmt.Errorf("target is required for a server forward tunnel")
			}
			if _, port, err := net.SplitHostPort(t.Target); err != nil {
				return fmt.Errorf("target: %w", err)
			} else if !validPort(port) {
				return fmt.Errorf("target: invalid port %q", port)
			}
		} else {
			if strings.TrimSpace(t.Listen) == "" {
				return fmt.Errorf("listen is required for a client forward tunnel")
			}
			if _, port, err := net.SplitHostPort(t.Listen); err != nil {
				return fmt.Errorf("listen: %w", err)
			} else if !validPort(port) {
				return fmt.Errorf("listen: invalid port %q", port)
			}
		}
	case TunnelExec:
		if strings.TrimSpace(t.Exec) == "" {
			return fmt.Errorf("exec is required for an exec tunnel")
		}
	}

	if t.Mode == ModeServer && t.Type != TunnelForward {
		// Forward carries no protocol-level authentication of its own
		// today (same as the CLI); everything privileged does.
		if !t.AllowUnauthenticated && strings.TrimSpace(t.TokenFile) == "" {
			return fmt.Errorf(
				"type %q requires token_file, or explicit allow_unauthenticated: true", t.Type)
		}
	}
	return nil
}

func validPort(text string) bool {
	value, err := strconv.Atoi(text)
	return err == nil && value >= 1 && value <= 65535
}

func orIndex(name string, i int) string {
	if name == "" {
		return fmt.Sprintf("#%d", i)
	}
	return name
}
