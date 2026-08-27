package httpapi

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jdbnet/vantage/internal/model"
)

func TestTCPOpen(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	if !tcpOpen("127.0.0.1", port, time.Second) {
		t.Fatal("expected listener to be up")
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	if tcpOpen("127.0.0.1", closedPort, 200*time.Millisecond) {
		t.Fatal("expected closed port to be down")
	}
}

func TestPingHosts(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	hosts := map[string]model.Host{
		"up":   {ID: "up", Hostname: "127.0.0.1", Port: port},
		"down": {ID: "down", Hostname: "127.0.0.1", Port: closedPort},
	}
	lookup := func(id string) (model.Host, error) {
		h, ok := hosts[id]
		if !ok {
			return model.Host{}, errors.New("missing")
		}
		return h, nil
	}
	got := pingHosts(lookup, []string{"up", "down", "missing"}, 300*time.Millisecond)
	if !got["up"].Up {
		t.Fatal("up host should be reachable")
	}
	if got["down"].Up {
		t.Fatal("down host should be unreachable")
	}
	if got["missing"].Up {
		t.Fatal("unknown id should be unreachable")
	}
}

func TestPingHostsViaJump(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	jumpID := "jump"
	hosts := map[string]model.Host{
		jumpID: {ID: jumpID, Hostname: "127.0.0.1", Port: port},
		"via": {
			ID:         "via",
			Hostname:   "203.0.113.9",
			Port:       22,
			JumpHostID: &jumpID,
		},
	}
	lookup := func(id string) (model.Host, error) {
		h, ok := hosts[id]
		if !ok {
			return model.Host{}, errors.New("missing")
		}
		return h, nil
	}
	got := pingHosts(lookup, []string{"via", jumpID}, 300*time.Millisecond)
	if !got["via"].Up || !got["via"].ViaJump {
		t.Fatalf("jump target: %+v", got["via"])
	}
	if !got[jumpID].Up || got[jumpID].ViaJump {
		t.Fatalf("direct jump host: %+v", got[jumpID])
	}
}
