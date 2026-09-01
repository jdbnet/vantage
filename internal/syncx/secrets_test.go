package syncx

import (
	"encoding/json"
	"testing"

	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
)

func testBox(t *testing.T) *cryptox.Box {
	t.Helper()
	dek, err := cryptox.NewDEK()
	if err != nil {
		t.Fatal(err)
	}
	return cryptox.Open(dek)
}

func TestRewriteHostInlinePasswordRoundTrip(t *testing.T) {
	src := testBox(t)
	dst := testBox(t)
	plain, _ := json.Marshal(model.IdentitySecret{Password: "vnc-secret"})
	blob, err := src.Encrypt(string(plain))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"id":                             "h1",
		"protocol":                       "vnc",
		"inline_identity_auth_type":      "password",
		"inline_identity_encrypted_blob": blob,
	})
	op := model.ChangeOp{Entity: "host", EntityID: "h1", Payload: payload}

	out := RewriteOp(src, op, true)
	var wire map[string]any
	if err := json.Unmarshal(out.Payload, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["inline_identity_encrypted_blob"]; ok {
		t.Fatal("outbound host should send inline_secret, not source ciphertext")
	}
	sec, ok := wire["inline_secret"].(map[string]any)
	if !ok {
		t.Fatalf("inline_secret: %T", wire["inline_secret"])
	}
	if sec["password"] != "vnc-secret" {
		t.Fatalf("password %v", sec["password"])
	}

	in := RewriteOp(dst, out, false)
	var stored map[string]any
	if err := json.Unmarshal(in.Payload, &stored); err != nil {
		t.Fatal(err)
	}
	gotBlob, _ := stored["inline_identity_encrypted_blob"].(string)
	if gotBlob == "" {
		t.Fatal("inbound host should store inline ciphertext")
	}
	if gotBlob == blob {
		t.Fatal("destination must re-encrypt under its own vault")
	}
	gotPlain, err := dst.Decrypt(gotBlob)
	if err != nil {
		t.Fatal(err)
	}
	var got model.IdentitySecret
	if err := json.Unmarshal([]byte(gotPlain), &got); err != nil {
		t.Fatal(err)
	}
	if got.Password != "vnc-secret" {
		t.Fatalf("got %+v", got)
	}
	if _, err := src.Decrypt(gotBlob); err == nil {
		t.Fatal("source vault should not decrypt destination ciphertext")
	}
}

func TestRewriteOpIgnoresUnknownEntity(t *testing.T) {
	box := testBox(t)
	op := model.ChangeOp{Entity: "folder", Payload: []byte(`{"id":"f1"}`)}
	got := RewriteOp(box, op, true)
	if string(got.Payload) != string(op.Payload) {
		t.Fatal("folder payload should be unchanged")
	}
}
