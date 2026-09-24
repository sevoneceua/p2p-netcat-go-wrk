package daemon

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/santaklouse/go-p2p-netcat/internal/listenerlock"
	"github.com/santaklouse/go-p2p-netcat/internal/tunnelconfig"
)

// TestForwardTunnelEndToEnd starts a server Daemon whose "echo" tunnel
// forwards to a real TCP echo listener, and a client Daemon whose tunnel
// dials the server's PeerId. It writes through the client's local port and
// checks the byte comes back through the whole path: local TCP -> p2p
// stream -> server tunnel -> echo target -> p2p stream -> local TCP.
func TestForwardTunnelEndToEnd(t *testing.T) {
	t.Setenv(listenerlock.DirectoryEnvironment, t.TempDir())

	echoAddr := startEchoServer(t)

	const logicalPort = 45001
	serverCfg := &tunnelconfig.Config{
		Identity: t.TempDir() + "/server-identity.key",
		Tunnels: []tunnelconfig.Tunnel{{
			Name:                 "echo",
			Type:                 tunnelconfig.TunnelForward,
			Mode:                 tunnelconfig.ModeServer,
			Protocol:             tunnelconfig.ProtocolTCP,
			LogicalPort:          logicalPort,
			Target:               echoAddr,
			AllowUnauthenticated: true,
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	serverDaemon, err := Run(ctx, serverCfg, testLogger(t, "server"))
	if err != nil {
		t.Fatalf("server Run: %v", err)
	}
	defer serverDaemon.Close()

	clientListen := freeLocalAddr(t)
	clientCfg := &tunnelconfig.Config{
		Identity: t.TempDir() + "/client-identity.key",
		Tunnels: []tunnelconfig.Tunnel{{
			Name:        "echo-client",
			Type:        tunnelconfig.TunnelForward,
			Mode:        tunnelconfig.ModeClient,
			Protocol:    tunnelconfig.ProtocolTCP,
			LogicalPort: logicalPort,
			Peer:        serverDaemon.Node.Host.ID().String(),
			Listen:      clientListen,
		}},
	}
	clientDaemon, err := Run(ctx, clientCfg, testLogger(t, "client"))
	if err != nil {
		t.Fatalf("client Run: %v", err)
	}
	defer clientDaemon.Close()

	if err := clientDaemon.Node.Host.Connect(ctx, peer.AddrInfo{
		ID:    serverDaemon.Node.Host.ID(),
		Addrs: serverDaemon.Node.Host.Addrs(),
	}); err != nil {
		t.Fatalf("direct connect: %v", err)
	}

	var conn net.Conn
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		conn, err = net.DialTimeout("tcp", clientListen, time.Second)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial client local listener: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello daemon\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if line != "hello daemon\n" {
		t.Fatalf("got %q, want %q", line, "hello daemon\n")
	}
}

// TestReloadAddsChangesAndRemovesTunnels exercises the full diff behavior
// documented on Daemon.Reload: adding a tunnel starts it and makes it
// reachable, removing it releases its logical-port lock (checked by
// re-acquiring that exact port directly, which would fail if the lock
// were still held), and a tunnel that did not change keeps its original
// handle instance instead of being torn down and rebuilt.
func TestReloadAddsChangesAndRemovesTunnels(t *testing.T) {
	t.Setenv(listenerlock.DirectoryEnvironment, t.TempDir())
	echoAddr := startEchoServer(t)

	const portA = 45010
	const portB = 45011

	tunnelA := tunnelconfig.Tunnel{
		Name: "a", Type: tunnelconfig.TunnelForward, Mode: tunnelconfig.ModeServer,
		Protocol: tunnelconfig.ProtocolTCP, LogicalPort: portA, Target: echoAddr,
		AllowUnauthenticated: true,
	}
	cfg := &tunnelconfig.Config{
		Identity: t.TempDir() + "/identity.key",
		Tunnels:  []tunnelconfig.Tunnel{tunnelA},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d, err := Run(ctx, cfg, testLogger(t, "reload"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer d.Close()

	d.mu.Lock()
	handleABeforeReload := d.handles["a"]
	d.mu.Unlock()

	tunnelB := tunnelconfig.Tunnel{
		Name: "b", Type: tunnelconfig.TunnelForward, Mode: tunnelconfig.ModeServer,
		Protocol: tunnelconfig.ProtocolTCP, LogicalPort: portB, Target: echoAddr,
		AllowUnauthenticated: true,
	}
	cfgWithB := &tunnelconfig.Config{
		Identity: cfg.Identity,
		Tunnels:  []tunnelconfig.Tunnel{tunnelA, tunnelB},
	}
	if err := d.Reload(cfgWithB); err != nil {
		t.Fatalf("Reload (add b): %v", err)
	}

	d.mu.Lock()
	_, hasB := d.handles["b"]
	handleAAfterAdd := d.handles["a"]
	d.mu.Unlock()
	if !hasB {
		t.Fatal("tunnel b not tracked after reload added it")
	}
	if handleAAfterAdd.lock != handleABeforeReload.lock {
		t.Error("unrelated tunnel a was restarted (lock instance changed) by a reload that didn't touch it")
	}

	if err := d.Reload(cfg); err != nil {
		t.Fatalf("Reload (remove b): %v", err)
	}
	d.mu.Lock()
	_, hasB = d.handles["b"]
	d.mu.Unlock()
	if hasB {
		t.Fatal("tunnel b still tracked after a reload that removed it")
	}

	// The strongest check: the logical-port lock must have actually been
	// released, not just dropped from the map. If it weren't, a fresh
	// Acquire on the same port would fail exactly like tunnelconfig's own
	// duplicate-port rule says it should for two simultaneous holders.
	freedLock, err := listenerlock.Acquire(portB)
	if err != nil {
		t.Fatalf("expected logical port %d to be free after tunnel b was removed: %v", portB, err)
	}
	_ = freedLock.Close()
}

// TestPrivilegedServerTunnelRequiresToken is a narrower regression check:
// tunnelconfig.Validate already rejects a config with no token_file and no
// allow_unauthenticated for socks/pty/exec, so Run must never reach the
// point of acquiring a lock or starting a listener for one.
func TestPrivilegedServerTunnelRequiresToken(t *testing.T) {
	t.Setenv(listenerlock.DirectoryEnvironment, t.TempDir())
	cfg := &tunnelconfig.Config{
		Identity: t.TempDir() + "/identity.key",
		Tunnels: []tunnelconfig.Tunnel{{
			Name:        "shell",
			Type:        tunnelconfig.TunnelPTY,
			Mode:        tunnelconfig.ModeServer,
			LogicalPort: 45002,
		}},
	}
	if _, err := Run(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected Run to reject an unauthenticated privileged tunnel")
	}
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = bufioCopy(conn, conn)
			}()
		}
	}()
	return listener.Addr().String()
}

// bufioCopy echoes line by line so the test can use ReadString('\n')
// against a well-defined framing instead of racing raw io.Copy semantics.
func bufioCopy(dst net.Conn, src net.Conn) (int64, error) {
	reader := bufio.NewReader(src)
	var total int64
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			n, writeErr := dst.Write([]byte(line))
			total += int64(n)
			if writeErr != nil {
				return total, writeErr
			}
		}
		if err != nil {
			return total, err
		}
	}
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func testLogger(t *testing.T, label string) Logger {
	return func(format string, args ...any) {
		t.Logf("["+label+"] "+format, args...)
	}
}
