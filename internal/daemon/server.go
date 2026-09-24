package daemon

import (
	"context"
	"fmt"
	"net"
	"strconv"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/santaklouse/go-p2p-netcat/internal/listenerlock"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	p2pnode "github.com/santaklouse/go-p2p-netcat/p2p"
	"github.com/santaklouse/go-p2p-netcat/protocol/admission"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	"github.com/santaklouse/go-p2p-netcat/session"
)

func (d *Daemon) startServer(ctx context.Context, t tunnelconfig.Tunnel, token *pairing.Token) (tunnelHandle, error) {
	lock, err := listenerlock.Acquire(t.LogicalPort)
	if err != nil {
		return tunnelHandle{}, fmt.Errorf("acquire logical port %d: %w", t.LogicalPort, err)
	}

	proto := p2pnode.ProtocolForService(t.LogicalPort)
	if t.Type == tunnelconfig.TunnelForward && t.Protocol == tunnelconfig.ProtocolUDP {
		proto = p2pnode.DatagramProtocolForService(t.LogicalPort)
	}

	d.Node.Host.SetStreamHandler(proto, func(stream libp2pnetwork.Stream) {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			defer stream.Close()
			if token != nil {
				if authErr := admission.AuthenticateServer(stream, token, authTimeout); authErr != nil {
					_ = stream.Reset()
					d.log("tunnel %s: pairing authentication from %s failed: %v",
						t.Name, stream.Conn().RemotePeer(), authErr)
					return
				}
			}
			d.log("tunnel %s: peer %s connected", t.Name, stream.Conn().RemotePeer())
			if sessionErr := runServerSession(ctx, stream, t); sessionErr != nil && ctx.Err() == nil {
				d.log("tunnel %s: session with %s ended: %v", t.Name, stream.Conn().RemotePeer(), sessionErr)
			}
		}()
	})

	// One Advertise loop per distinct token (each token derives its own
	// rendezvous CIDs — see p2p.Node.Advertise), mirroring what a
	// single-tunnel CLI process would do for this tunnel. A tunnel with
	// no token still gets the plain PeerId advertisement.
	d.Node.Advertise(ctx, token)

	d.log("tunnel %s: listening on logical port %d (%s/%s)", t.Name, t.LogicalPort, t.Type, protocolLabel(t))
	return tunnelHandle{lock: lock, proto: proto}, nil
}

func protocolLabel(t tunnelconfig.Tunnel) string {
	if t.Type == tunnelconfig.TunnelForward {
		return string(t.Protocol)
	}
	return "tcp"
}

// runServerSession is the daemon's equivalent of internal/cli's
// runServerSession, dispatching on tunnelconfig.TunnelType instead of the
// CLI's *options flags. tunnelconfig.Validate already guarantees Target/
// Exec/Protocol are set appropriately for t.Type by the time a Tunnel
// reaches here.
func runServerSession(ctx context.Context, stream session.Stream, t tunnelconfig.Tunnel) error {
	switch t.Type {
	case tunnelconfig.TunnelForward:
		host, portText, err := net.SplitHostPort(t.Target)
		if err != nil {
			return fmt.Errorf("target: %w", err)
		}
		if host == "" {
			host = "127.0.0.1"
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return fmt.Errorf("target port: %w", err)
		}
		if t.Protocol == tunnelconfig.ProtocolUDP {
			return session.UDPForward(ctx, stream, host, port, dialTimeout, session.DefaultUDPIdleTimeout)
		}
		return session.TCPForward(ctx, stream, host, port, dialTimeout)
	case tunnelconfig.TunnelSOCKS:
		return session.SOCKS(ctx, stream, dialTimeout)
	case tunnelconfig.TunnelPTY:
		return session.PTYServer(ctx, stream, false)
	case tunnelconfig.TunnelExec:
		return session.Exec(ctx, stream, t.Exec, false)
	default:
		return fmt.Errorf("unsupported tunnel type %q", t.Type)
	}
}
