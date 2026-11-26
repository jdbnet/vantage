package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jdbnet/vantage/internal/explorer"
	"github.com/jdbnet/vantage/internal/protocols"
	"github.com/jdbnet/vantage/internal/sshx"
	"golang.org/x/crypto/ssh"
)

const remoteIdle = 3 * time.Minute

type remoteSecret struct {
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase,omitempty"`
}

type remotePool struct {
	s     *Server
	mu    sync.Mutex
	items map[string]*remoteConn
	dial  func(explorer.Config) (explorer.Explorer, error)
	load  func(string) (explorer.Config, error)
	stop  chan struct{}
}

type remoteConn struct {
	exp       explorer.Explorer
	exclusive bool
	op        sync.Mutex
	refs      int
	last      time.Time
	retire    bool
}

func newRemotePool(s *Server) *remotePool {
	p := &remotePool{
		s:     s,
		items: map[string]*remoteConn{},
		stop:  make(chan struct{}),
	}
	p.dial = p.dialRemote
	p.load = p.loadRemote
	go p.sweepLoop()
	return p
}

func (p *remotePool) sweepLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.sweep()
		}
	}
}

func (p *remotePool) sweep() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for id, c := range p.items {
		if c.refs == 0 && now.Sub(c.last) >= remoteIdle {
			_ = c.exp.Disconnect()
			delete(p.items, id)
		}
	}
}

func (p *remotePool) drop(id string) {
	p.mu.Lock()
	c := p.items[id]
	delete(p.items, id)
	if c != nil && c.refs > 0 {
		c.retire = true
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if c != nil {
		_ = c.exp.Disconnect()
	}
}

func (p *remotePool) loadRemote(id string) (explorer.Config, error) {
	if p.s == nil || p.s.d.Store == nil {
		return explorer.Config{}, errors.New("remotes unavailable")
	}
	box := p.s.d.Box()
	if box == nil {
		return explorer.Config{}, errors.New("vault is locked; sign in first")
	}
	rec, err := p.s.d.Store.GetRemote(id)
	if err != nil {
		return explorer.Config{}, err
	}
	cfg := explorer.Config{
		ID: rec.Remote.ID, Label: rec.Remote.Label, Protocol: rec.Remote.Protocol,
		Host: rec.Remote.Host, Port: rec.Remote.Port, Bucket: rec.Remote.Bucket,
		Region: rec.Remote.Region, PathStyle: rec.Remote.PathStyle, Secure: rec.Remote.Secure,
		Username: rec.Remote.Username, SftpKeyAuth: rec.Remote.SftpKeyAuth,
	}
	if rec.Blob != "" {
		plain, err := box.Decrypt(rec.Blob)
		if err != nil {
			return explorer.Config{}, err
		}
		var sec remoteSecret
		if err := json.Unmarshal([]byte(plain), &sec); err != nil {
			return explorer.Config{}, err
		}
		cfg.Secret = sec.Secret
		cfg.Passphrase = sec.Passphrase
	}
	return cfg, nil
}

func (p *remotePool) dialRemote(cfg explorer.Config) (explorer.Explorer, error) {
	var cb ssh.HostKeyCallback
	if cfg.Protocol == "sftp" {
		host := cfg.Host
		port := cfg.Port
		if port == 0 {
			port = 22
		}
		cb = func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if p.s == nil || p.s.d.Store == nil {
				return fmt.Errorf("host key for %s:%d is not trusted", host, port)
			}
			return sshx.CheckHostKey(p.s.d.Store, host, port, key)
		}
	}
	return protocols.Open(cfg, cb)
}

func (p *remotePool) exclusive(id string) bool {
	cfg, err := p.load(id)
	if err != nil {
		return true
	}
	return explorer.Exclusive(cfg.Protocol)
}

// acquire returns a connection. dedicated is true for transfers and streams:
// protocols that cannot share get a fresh connection that the caller closes.
func (p *remotePool) acquire(id string, dedicated bool) (explorer.Explorer, func(), error) {
	load := p.load
	dial := p.dial
	if load == nil || dial == nil {
		return nil, nil, errors.New("remotes unavailable")
	}
	cfg, err := load(id)
	if err != nil {
		return nil, nil, err
	}
	if dedicated && explorer.Exclusive(cfg.Protocol) {
		exp, err := dial(cfg)
		if err != nil {
			return nil, nil, err
		}
		return exp, func() { _ = exp.Disconnect() }, nil
	}
	p.mu.Lock()
	c := p.items[id]
	p.mu.Unlock()
	if c == nil {
		exp, err := dial(cfg)
		if err != nil {
			return nil, nil, err
		}
		p.mu.Lock()
		if existing := p.items[id]; existing != nil {
			p.mu.Unlock()
			_ = exp.Disconnect()
			c = existing
		} else {
			c = &remoteConn{
				exp:       exp,
				exclusive: explorer.Exclusive(cfg.Protocol),
				last:      time.Now(),
			}
			p.items[id] = c
			p.mu.Unlock()
		}
	}
	p.mu.Lock()
	c.refs++
	c.last = time.Now()
	p.mu.Unlock()
	if c.exclusive {
		c.op.Lock()
	}
	release := func() {
		if c.exclusive {
			c.op.Unlock()
		}
		p.mu.Lock()
		c.refs--
		c.last = time.Now()
		retire := c.retire && c.refs == 0
		p.mu.Unlock()
		if retire {
			_ = c.exp.Disconnect()
		}
	}
	return c.exp, release, nil
}
