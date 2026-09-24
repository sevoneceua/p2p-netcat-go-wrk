package daemon

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
	"github.com/santaklouse/go-p2p-netcat/protocol/admission"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	"github.com/santaklouse/go-p2p-netcat/session"
)

func (d *Daemon) startClient(ctx context.Context, t tunnelconfig.Tunnel, token *pairing.Token) error {
	if t.Type != tunnelconfig.TunnelForward {
		// tunnelconfig.Validate already rejects this; kept as a direct
		// error (not a panic) in case a Config reaches here unvalidated.
		return fmt.Errorf("client mode only supports type forward, got %q", t.Type)
	}
	host, portText, err := net.SplitHostPort(t.Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("listen port: %w", err)
	}

	openStream := func(openCtx context.Context) (session.Stream, error) {
		dialCtx, cancel := context.WithTimeout(openCtx, dialTimeout)
		defer cancel()
		var stream session.Stream
		var err error
		if t.Protocol == tunnelconfig.ProtocolUDP {
			stream, err = d.Node.OpenDatagramStream(dialCtx, t.Peer, t.LogicalPort, nil, token)
		} else {
			stream, err = d.Node.OpenStream(dialCtx, t.Peer, t.LogicalPort, nil, token)
		}
		if err != nil {
			return nil, err
		}
		if token != nil {
			if authErr := admission.AuthenticateClient(stream, token, authTimeout); authErr != nil {
				_ = stream.Reset()
				return nil, authErr
			}
		}
		return stream, nil
	}
	onError := func(err error) {
		if ctx.Err() == nil {
			d.log("tunnel %s: %v", t.Name, err)
		}
	}

	if t.Protocol == tunnelconfig.ProtocolUDP {
		// Same reasoning as internal/cli/root.go's client path: establish
		// the first p2p carrier before returning, rather than waiting for
		// the first local datagram to trigger discovery.
		preconnected, err := openStream(ctx)
		if err != nil {
			return fmt.Errorf("establish initial UDP carrier: %w", err)
		}
		opener := newPreconnectedOpener(preconnected, openStream)
		listener, err := session.StartLocalUDPForward(
			ctx, host, port, session.DefaultUDPIdleTimeout, opener.Open, onError)
		if err != nil {
			_ = opener.Close()
			return err
		}
		d.addCloser(listener)
		d.log("tunnel %s: local UDP %s -> %s service %d", t.Name, listener.LocalAddr(), t.Peer, t.LogicalPort)
		return nil
	}

	listener, err := session.StartLocalForward(ctx, host, port, openStream, onError)
	if err != nil {
		return err
	}
	d.addCloser(listener)
	d.log("tunnel %s: local %s -> %s service %d", t.Name, listener.Addr(), t.Peer, t.LogicalPort)
	return nil
}

// preconnectedOpener hands out an already-open stream once, then falls
// back to fallback for every subsequent call. Local copy of the same
// pattern internal/cli/root.go uses for UDP client forwards (that one is
// unexported and CLI-specific, so it isn't reusable directly here).
type preconnectedOpener struct {
	mu       sync.Mutex
	first    session.Stream
	fallback func(context.Context) (session.Stream, error)
}

func newPreconnectedOpener(
	first session.Stream,
	fallback func(context.Context) (session.Stream, error),
) *preconnectedOpener {
	return &preconnectedOpener{first: first, fallback: fallback}
}

func (o *preconnectedOpener) Open(ctx context.Context) (session.Stream, error) {
	o.mu.Lock()
	if o.first != nil {
		stream := o.first
		o.first = nil
		o.mu.Unlock()
		return stream, nil
	}
	o.mu.Unlock()
	return o.fallback(ctx)
}

func (o *preconnectedOpener) Close() error {
	o.mu.Lock()
	stream := o.first
	o.first = nil
	o.mu.Unlock()
	if stream != nil {
		return stream.Close()
	}
	return nil
}
