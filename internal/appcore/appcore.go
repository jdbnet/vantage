package appcore

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jdbnet/vantage/internal/auth"
	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/httpapi"
	"github.com/jdbnet/vantage/internal/sshx"
	"github.com/jdbnet/vantage/internal/store"
	"github.com/jdbnet/vantage/internal/syncx"
)

var Version = "dev"

type Core struct {
	Store      *store.Store
	Jar        *auth.Jar
	Conns      *sshx.Registry
	DataDir    string
	Mode       string
	Listen     string
	Version    string
	mu         sync.RWMutex
	box        *cryptox.Box
	syncClient *syncx.Client
	closed     sync.Once
}

func Open(dataDir, mode, listen string, cookieSecure bool) (*Core, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(dataDir, "vantage.db"))
	if err != nil {
		return nil, err
	}
	secret, err := loadOrCreateSecret(filepath.Join(dataDir, "session.key"))
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	c := &Core{
		Store:   st,
		Jar:     auth.NewJar(secret, 0, cookieSecure),
		Conns:   sshx.NewRegistry(),
		DataDir: dataDir,
		Mode:    mode,
		Listen:  listen,
		Version: Version,
	}
	if dek, err := cryptox.ReadDEKFile(filepath.Join(dataDir, "dek")); err == nil {
		c.box = cryptox.Open(dek)
	}
	settings, _ := st.LoadSettings()
	if listen == "" {
		if settings.ListenAddr != "" {
			c.Listen = settings.ListenAddr
		} else if mode == "desktop" {
			c.Listen = "127.0.0.1:7688"
		} else {
			c.Listen = ":7687"
		}
	}
	vault := "locked"
	if c.box != nil {
		vault = "unlocked"
	}
	log.Printf("core ready mode=%s listen=%s data=%s vault=%s", c.Mode, c.Listen, dataDir, vault)
	return c, nil
}

func (c *Core) Close() error {
	var err error
	c.closed.Do(func() {
		log.Printf("core closing mode=%s", c.Mode)
		if c.syncClient != nil {
			c.syncClient.Stop()
		}
		c.Conns.CloseAll()
		err = c.Store.Close()
	})
	return err
}

func (c *Core) Box() *cryptox.Box {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.box
}

func (c *Core) SetBox(b *cryptox.Box) {
	c.mu.Lock()
	c.box = b
	c.mu.Unlock()
}

func (c *Core) Handler() http.Handler {
	return httpapi.New(httpapi.Deps{
		Store:           c.Store,
		Box:             c.Box,
		SetBox:          c.SetBox,
		Jar:             c.Jar,
		Conns:           c.Conns,
		DataDir:         c.DataDir,
		Mode:            c.Mode,
		Listen:          c.Listen,
		Version:         c.Version,
		NeedsSetup:      c.NeedsSetup,
		Setup:           c.Setup,
		Login:           c.Login,
		ChangePassword:  c.ChangePassword,
		OnSettingsSaved: c.StartSyncClient,
		SyncStatus:      c.SyncStatus,
		KickSync:        c.KickSync,
	})
}

func (c *Core) NeedsSetup() bool {
	_, ok, _ := c.Store.Meta("operator_username")
	return !ok
}

func (c *Core) Setup(username, password string) error {
	hash, err := cryptox.HashPassword(password)
	if err != nil {
		return err
	}
	dek, err := cryptox.NewDEK()
	if err != nil {
		return err
	}
	wrapped, err := cryptox.WrapDEK(password, dek)
	if err != nil {
		return err
	}
	if err := c.Store.SetMeta("operator_username", username); err != nil {
		return err
	}
	if err := c.Store.SetMeta("operator_password_hash", hash); err != nil {
		return err
	}
	if err := c.Store.SetMeta("wrapped_dek", wrapped); err != nil {
		return err
	}
	if err := cryptox.WriteDEKFile(filepath.Join(c.DataDir, "dek"), dek); err != nil {
		return err
	}
	c.SetBox(cryptox.Open(dek))
	return nil
}

func (c *Core) Login(username, password string) error {
	user, ok, err := c.Store.Meta("operator_username")
	if err != nil {
		return err
	}
	if !ok || user != username {
		return fmt.Errorf("invalid credentials")
	}
	hash, ok, err := c.Store.Meta("operator_password_hash")
	if err != nil || !ok || !cryptox.VerifyPassword(password, hash) {
		return fmt.Errorf("invalid credentials")
	}
	if c.Box() == nil {
		if wrapped, ok, _ := c.Store.Meta("wrapped_dek"); ok {
			dek, err := cryptox.UnwrapDEK(password, wrapped)
			if err != nil {
				return fmt.Errorf("invalid credentials")
			}
			_ = cryptox.WriteDEKFile(filepath.Join(c.DataDir, "dek"), dek)
			c.SetBox(cryptox.Open(dek))
		}
	}
	return nil
}

func (c *Core) ChangePassword(current, newPassword string) error {
	if strings.TrimSpace(newPassword) == "" {
		return fmt.Errorf("new password required")
	}
	hash, ok, err := c.Store.Meta("operator_password_hash")
	if err != nil || !ok || !cryptox.VerifyPassword(current, hash) {
		return fmt.Errorf("invalid credentials")
	}
	box := c.Box()
	if box == nil {
		return fmt.Errorf("vault locked")
	}
	dek := box.DEK()
	if len(dek) == 0 {
		return fmt.Errorf("vault locked")
	}
	newHash, err := cryptox.HashPassword(newPassword)
	if err != nil {
		return err
	}
	wrapped, err := cryptox.WrapDEK(newPassword, dek)
	if err != nil {
		return err
	}
	if err := c.Store.SetMeta("operator_password_hash", newHash); err != nil {
		return err
	}
	return c.Store.SetMeta("wrapped_dek", wrapped)
}

func (c *Core) StartSyncClient() {
	if c.Mode != "desktop" {
		return
	}
	c.mu.Lock()
	old := c.syncClient
	c.syncClient = nil
	c.mu.Unlock()
	if old != nil {
		old.Stop()
	}
	sc := syncx.StartClient(c.Store, c.Box, c.DataDir)
	c.mu.Lock()
	c.syncClient = sc
	c.mu.Unlock()
}

func (c *Core) SyncStatus() syncx.Status {
	if c.Mode != "desktop" {
		return syncx.Status{Enabled: false}
	}
	c.mu.RLock()
	sc := c.syncClient
	c.mu.RUnlock()
	if sc == nil {
		return syncx.Status{Enabled: true}
	}
	return sc.Status()
}

func (c *Core) KickSync() error {
	if c.Mode != "desktop" {
		return fmt.Errorf("sync client is only available on the desktop app")
	}
	c.mu.RLock()
	sc := c.syncClient
	c.mu.RUnlock()
	if sc == nil {
		c.StartSyncClient()
		return nil
	}
	sc.Kick()
	return nil
}

func loadOrCreateSecret(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 16 {
		return b, nil
	}
	b = make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return b, nil
}
