package tunnelconfig

import "testing"

const validYAML = `
identity: ~/.config/p2p-netcat/identity.key
relays:
  - addr: /ip4/203.0.113.10/tcp/4001/p2p/12D3KooWRelayOne
    priority: 1
  - addr: /ip4/203.0.113.20/tcp/4001/p2p/12D3KooWRelayTwo
    priority: 2
fallback_to_public_dht: true
upstream_socks: 127.0.0.1:9050

tunnels:
  - name: postgres-server
    type: forward
    mode: server
    protocol: tcp
    logical_port: 15432
    target: 127.0.0.1:5432
    token_file: ~/.config/p2p-netcat/tokens/postgres.token

  - name: postgres-client
    type: forward
    mode: client
    protocol: tcp
    logical_port: 15432
    peer: 12D3KooWExamplePeerID
    listen: 127.0.0.1:15432
    token_file: ~/.config/p2p-netcat/tokens/postgres.token

  - name: exit-proxy
    type: socks
    mode: server
    logical_port: 10800
    token_file: ~/.config/p2p-netcat/tokens/socks.token

  - name: admin-shell
    type: pty
    mode: server
    logical_port: 2222
    allow_unauthenticated: false
    token_file: ~/.config/p2p-netcat/tokens/shell.token
`

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Tunnels) != 4 {
		t.Fatalf("tunnels = %d, want 4", len(cfg.Tunnels))
	}
	if !cfg.FallbackEnabled() {
		t.Fatal("FallbackEnabled() = false, want true")
	}
	if cfg.Relays[0].Priority != 1 || cfg.Relays[1].Priority != 2 {
		t.Fatalf("relay priorities = %+v", cfg.Relays)
	}
}

func TestFallbackDefaultsTrue(t *testing.T) {
	cfg, err := Parse([]byte(`
tunnels:
  - name: t1
    type: forward
    mode: client
    protocol: tcp
    logical_port: 1
    peer: 12D3KooWExamplePeerID
    listen: 127.0.0.1:1234
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.FallbackEnabled() {
		t.Fatal("FallbackEnabled() = false, want default true")
	}
}

func TestValidateRejectsEmptyTunnels(t *testing.T) {
	_, err := Parse([]byte(`tunnels: []`))
	if err == nil {
		t.Fatal("expected error for empty tunnels")
	}
}

func TestValidateRejectsDuplicateServerPort(t *testing.T) {
	_, err := Parse([]byte(`
tunnels:
  - name: a
    type: forward
    mode: server
    protocol: tcp
    logical_port: 100
    target: 127.0.0.1:80
    allow_unauthenticated: true
  - name: b
    type: socks
    mode: server
    logical_port: 100
    allow_unauthenticated: true
`))
	if err == nil {
		t.Fatal("expected error for duplicate server logical_port")
	}
}

func TestValidateAllowsSharedPortAcrossClientAndServer(t *testing.T) {
	// A client tunnel dialing service 100 on a different peer does not
	// contend with a local server tunnel also numbered 100: the
	// listenerlock only guards local server listeners.
	_, err := Parse([]byte(`
tunnels:
  - name: local-server
    type: forward
    mode: server
    protocol: tcp
    logical_port: 100
    target: 127.0.0.1:80
    allow_unauthenticated: true
  - name: remote-client
    type: forward
    mode: client
    protocol: tcp
    logical_port: 100
    peer: 12D3KooWExamplePeerID
    listen: 127.0.0.1:9000
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDuplicateName(t *testing.T) {
	_, err := Parse([]byte(`
tunnels:
  - name: dup
    type: forward
    mode: server
    protocol: tcp
    logical_port: 1
    target: 127.0.0.1:80
    allow_unauthenticated: true
  - name: dup
    type: forward
    mode: server
    protocol: tcp
    logical_port: 2
    target: 127.0.0.1:81
    allow_unauthenticated: true
`))
	if err == nil {
		t.Fatal("expected error for duplicate tunnel name")
	}
}

func TestValidateClientRequiresPeer(t *testing.T) {
	_, err := Parse([]byte(`
tunnels:
  - name: t1
    type: forward
    mode: client
    protocol: tcp
    logical_port: 1
    listen: 127.0.0.1:1234
`))
	if err == nil {
		t.Fatal("expected error for missing peer in client mode")
	}
}

func TestValidateServerOnlyTypesRejectClientMode(t *testing.T) {
	for _, tunnelType := range []TunnelType{TunnelSOCKS, TunnelPTY, TunnelExec} {
		yamlDoc := `
tunnels:
  - name: t1
    type: ` + string(tunnelType) + `
    mode: client
    logical_port: 1
    peer: 12D3KooWExamplePeerID
`
		if _, err := Parse([]byte(yamlDoc)); err == nil {
			t.Fatalf("type %q: expected error in client mode", tunnelType)
		}
	}
}

func TestValidateExecRequiresCommand(t *testing.T) {
	_, err := Parse([]byte(`
tunnels:
  - name: t1
    type: exec
    mode: server
    logical_port: 1
    allow_unauthenticated: true
`))
	if err == nil {
		t.Fatal("expected error for missing exec command")
	}
}

func TestValidatePrivilegedServerRequiresAuth(t *testing.T) {
	_, err := Parse([]byte(`
tunnels:
  - name: t1
    type: exec
    mode: server
    logical_port: 1
    exec: /bin/true
`))
	if err == nil {
		t.Fatal("expected error: exec server tunnel with no token_file and no allow_unauthenticated")
	}
}

func TestValidateRejectsBadUpstreamSOCKS(t *testing.T) {
	_, err := Parse([]byte(`
upstream_socks: not-a-host-port
tunnels:
  - name: t1
    type: forward
    mode: client
    protocol: tcp
    logical_port: 1
    peer: 12D3KooWExamplePeerID
    listen: 127.0.0.1:1234
`))
	if err == nil {
		t.Fatal("expected error for malformed upstream_socks")
	}
}

func TestValidateForwardRequiresProtocol(t *testing.T) {
	_, err := Parse([]byte(`
tunnels:
  - name: t1
    type: forward
    mode: server
    logical_port: 1
    target: 127.0.0.1:80
    allow_unauthenticated: true
`))
	if err == nil {
		t.Fatal("expected error for missing protocol on forward tunnel")
	}
}
