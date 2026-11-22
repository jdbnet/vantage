package httpapi

import (
	"net/http"
	"strings"
	"sync"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/sshx"
)

type quickTarget struct {
	Protocol   model.Protocol
	Hostname   string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	Passphrase string
	Label      string
}

func (t quickTarget) host() model.Host {
	return model.Host{
		Label:    t.Label,
		Hostname: t.Hostname,
		Port:     t.Port,
		Protocol: t.Protocol,
	}
}

func (t quickTarget) creds() sshx.Creds {
	return sshx.Creds{
		User:       t.Username,
		Password:   t.Password,
		PrivateKey: t.PrivateKey,
		Passphrase: t.Passphrase,
	}
}

type quickBook struct {
	mu sync.Mutex
	m  map[string]quickTarget
}

func (b *quickBook) put(t quickTarget) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]quickTarget{}
	}
	id := "q-" + idgen.New()
	b.m[id] = t
	return id
}

func (b *quickBook) get(id string) (quickTarget, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.m[id]
	return t, ok
}

func (s *Server) handleQuickConnect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Protocol   string `json:"protocol"`
		Hostname   string `json:"hostname"`
		Port       int    `json:"port"`
		Username   string `json:"ssh_username"`
		Password   string `json:"password"`
		PrivateKey string `json:"private_key"`
		Passphrase string `json:"key_passphrase"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	proto := model.Protocol(strings.TrimSpace(body.Protocol))
	if proto != model.ProtocolSSH && proto != model.ProtocolVNC && proto != model.ProtocolRDP {
		proto = model.ProtocolSSH
	}
	host := strings.TrimSpace(body.Hostname)
	if host == "" {
		writeErr(w, 400, "hostname required")
		return
	}
	port := body.Port
	if port <= 0 {
		switch proto {
		case model.ProtocolVNC:
			port = 5900
		case model.ProtocolRDP:
			port = 3389
		default:
			port = 22
		}
	}
	user := strings.TrimSpace(body.Username)
	if user == "" && proto != model.ProtocolVNC {
		writeErr(w, 400, "username required")
		return
	}
	if proto == model.ProtocolSSH && body.Password == "" && strings.TrimSpace(body.PrivateKey) == "" {
		writeErr(w, 400, "password or private key required")
		return
	}
	if proto != model.ProtocolSSH && body.Password == "" && proto != model.ProtocolVNC {
		writeErr(w, 400, "password required")
		return
	}
	id := s.quick.put(quickTarget{
		Protocol:   proto,
		Hostname:   host,
		Port:       port,
		Username:   user,
		Password:   body.Password,
		PrivateKey: body.PrivateKey,
		Passphrase: body.Passphrase,
		Label:      host,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "protocol": proto, "label": host})
}
