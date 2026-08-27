package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/jdbnet/vantage/internal/store"
	"golang.org/x/crypto/ssh"
)

func testPub(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestCheckHostKeyAcceptsNew(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	key := testPub(t)
	prompt := &Prompt{
		HostKey: func(info HostKeyInfo) (HostKeyDecision, error) {
			if info.Status != "new" {
				t.Fatalf("status %s", info.Status)
			}
			return HostKeyDecision{Accept: true}, nil
		},
	}
	if err := checkHostKey(st, prompt, "example.test", 22, key); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetKnownHost("example.test", 22)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("fingerprint %s", got.Fingerprint)
	}
	if err := checkHostKey(st, nil, "example.test", 22, key); err != nil {
		t.Fatal(err)
	}
}

func TestCheckHostKeyMismatchReplace(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	oldKey := testPub(t)
	newKey := testPub(t)
	if err := checkHostKey(st, &Prompt{
		HostKey: func(HostKeyInfo) (HostKeyDecision, error) {
			return HostKeyDecision{Accept: true}, nil
		},
	}, "example.test", 22, oldKey); err != nil {
		t.Fatal(err)
	}
	if err := checkHostKey(st, &Prompt{
		HostKey: func(info HostKeyInfo) (HostKeyDecision, error) {
			if info.Status != "mismatch" {
				t.Fatalf("status %s", info.Status)
			}
			return HostKeyDecision{Accept: true, Replace: true}, nil
		},
	}, "example.test", 22, newKey); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetKnownHost("example.test", 22)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != ssh.FingerprintSHA256(newKey) {
		t.Fatalf("fingerprint %s", got.Fingerprint)
	}
}
