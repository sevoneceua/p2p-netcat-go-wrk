package p2p

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func TestApplyUpstreamSOCKSGuardForcesOffLeakyTransports(t *testing.T) {
	cfg := applyUpstreamSOCKSGuard(Config{
		UpstreamSOCKS: "127.0.0.1:9050",
		EnableQUIC:    true,
		EnableWebRTC:  true,
	})
	if cfg.EnableQUIC {
		t.Error("EnableQUIC should be forced off when UpstreamSOCKS is set")
	}
	if cfg.EnableWebRTC {
		t.Error("EnableWebRTC should be forced off when UpstreamSOCKS is set")
	}
}

func TestApplyUpstreamSOCKSGuardNoOpWithoutProxy(t *testing.T) {
	cfg := applyUpstreamSOCKSGuard(Config{
		EnableQUIC:   true,
		EnableWebRTC: true,
	})
	if !cfg.EnableQUIC || !cfg.EnableWebRTC {
		t.Error("guard must not touch transports when UpstreamSOCKS is empty")
	}
}

func TestNewRejectsMalformedUpstreamSOCKS(t *testing.T) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(context.Background(), Config{
		PrivateKey:    key,
		UpstreamSOCKS: "not-a-host-port",
	})
	if err == nil {
		t.Fatal("expected error for malformed upstream_socks address")
	}
}

func TestNewWithUpstreamSOCKSStillConstructsAHost(t *testing.T) {
	// No live SOCKS proxy needed here: WithDialerForAddr only invokes
	// the dialer when an actual outbound connection is attempted, and
	// this test never dials out. It just checks that a syntactically
	// valid upstream_socks address doesn't prevent Host construction,
	// and that QUIC/WebRTC being requested alongside it doesn't error
	// (they are silently disabled instead, per applyUpstreamSOCKSGuard).
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	node, err := New(context.Background(), Config{
		PrivateKey:    key,
		IPVersion:     4,
		UpstreamSOCKS: "127.0.0.1:9050",
		EnableQUIC:    true,
		EnableWebRTC:  true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })
	if node.Host == nil {
		t.Fatal("Host is nil")
	}
}
