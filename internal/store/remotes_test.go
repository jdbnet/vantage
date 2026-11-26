package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteCRUDHidesSecret(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	id, err := st.CreateRemote(RemoteWrite{
		Label: "nas", Protocol: "sftp", Host: "nas.example", Port: 22,
		Username: "jamie", Blob: "ciphertext-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.ListRemotes()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(items)
	if string(raw) == "" || strings.Contains(string(raw), "ciphertext-secret") {
		t.Fatalf("secret leaked: %s", raw)
	}
	if len(items) != 1 || !items[0].HasSecret {
		t.Fatalf("list %+v", items)
	}
	got, err := st.GetRemote(id)
	if err != nil || got.Blob != "ciphertext-secret" {
		t.Fatalf("blob %q %v", got.Blob, err)
	}
	if err := st.UpdateRemote(id, RemoteWrite{
		Label: "nas-2", Protocol: "sftp", Host: "nas.example", Port: 22, Username: "jamie",
	}); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetRemote(id)
	if err != nil || got.Blob != "ciphertext-secret" || got.Remote.Label != "nas-2" {
		t.Fatalf("kept %+v %v", got, err)
	}
	if err := st.DeleteRemote(id); err != nil {
		t.Fatal(err)
	}
	items, err = st.ListRemotes()
	if err != nil || len(items) != 0 {
		t.Fatalf("after delete %+v %v", items, err)
	}
}
