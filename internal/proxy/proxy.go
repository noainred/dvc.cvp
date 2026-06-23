// Package proxy provides the network dialer used to reach remote data centers.
// A site can be reached directly or tunneled through an SSH jump host, which is
// the model used here: the Korea HQ collector opens an SSH connection to each
// site's bastion and dials the site-local CloudVision Portal / switches through
// it. The resulting Dialer is plugged into the HTTP transport of the CVP/eAPI
// clients so all traffic transparently flows through the tunnel.
package proxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/noainred/dvc.cvp/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Dialer establishes TCP connections to a target, possibly through a tunnel.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	Close() error
}

// New builds a Dialer for the given proxy configuration.
func New(cfg config.ProxyConfig) (Dialer, error) {
	switch cfg.Type {
	case "", "direct":
		return &directDialer{d: &net.Dialer{Timeout: 10 * time.Second}}, nil
	case "ssh":
		return newSSHDialer(cfg)
	default:
		return nil, fmt.Errorf("unknown proxy type %q", cfg.Type)
	}
}

// directDialer dials without any tunnel.
type directDialer struct{ d *net.Dialer }

func (x *directDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return x.d.DialContext(ctx, network, addr)
}
func (x *directDialer) Close() error { return nil }

// sshDialer lazily maintains a single SSH connection to a jump host and dials
// targets through it. The connection is re-established automatically if it drops.
type sshDialer struct {
	cfg     config.ProxyConfig
	auth    []ssh.AuthMethod
	hostKey ssh.HostKeyCallback

	mu     sync.Mutex
	client *ssh.Client
}

func newSSHDialer(cfg config.ProxyConfig) (*sshDialer, error) {
	if cfg.JumpHost == "" {
		return nil, fmt.Errorf("ssh proxy requires jumpHost")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("ssh proxy requires user")
	}

	var auth []ssh.AuthMethod
	if cfg.KeyFile != "" {
		key, err := os.ReadFile(expand(cfg.KeyFile))
		if err != nil {
			return nil, fmt.Errorf("read ssh key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("ssh proxy requires keyFile or password")
	}

	hostKey := ssh.InsecureIgnoreHostKey() //nolint:gosec // fallback when no known_hosts configured
	if cfg.KnownHosts != "" {
		cb, err := knownhosts.New(expand(cfg.KnownHosts))
		if err != nil {
			return nil, fmt.Errorf("load known_hosts: %w", err)
		}
		hostKey = cb
	}

	return &sshDialer{cfg: cfg, auth: auth, hostKey: hostKey}, nil
}

func (x *sshDialer) connect() (*ssh.Client, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.client != nil {
		return x.client, nil
	}
	cc := &ssh.ClientConfig{
		User:            x.cfg.User,
		Auth:            x.auth,
		HostKeyCallback: x.hostKey,
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", x.cfg.JumpHost, cc)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", x.cfg.JumpHost, err)
	}
	x.client = client
	return client, nil
}

func (x *sshDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	client, err := x.connect()
	if err != nil {
		return nil, err
	}
	conn, err := client.DialContext(ctx, network, addr)
	if err != nil {
		// The tunnel may have dropped; reset and try once more.
		x.mu.Lock()
		if x.client != nil {
			_ = x.client.Close()
			x.client = nil
		}
		x.mu.Unlock()
		if client, err2 := x.connect(); err2 == nil {
			return client.DialContext(ctx, network, addr)
		}
		return nil, fmt.Errorf("tunnel dial %s: %w", addr, err)
	}
	return conn, nil
}

func (x *sshDialer) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.client != nil {
		err := x.client.Close()
		x.client = nil
		return err
	}
	return nil
}

func expand(path string) string {
	if len(path) > 1 && path[0] == '~' && (path[1] == '/' || path[1] == os.PathSeparator) {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}
