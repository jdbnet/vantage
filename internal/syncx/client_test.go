package syncx

import "testing"

func TestKickAndStopWithoutWS(t *testing.T) {
	c := &Client{
		stop: make(chan struct{}),
		kick: make(chan struct{}, 1),
	}
	c.Kick()
	c.Stop()
	c.Stop()
	st := c.Status()
	if !st.Enabled {
		t.Fatal("expected enabled client status")
	}
}
