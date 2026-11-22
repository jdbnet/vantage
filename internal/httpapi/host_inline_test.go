package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
)

func TestSecretUsernameOmitsPassword(t *testing.T) {
	dek, err := cryptox.NewDEK()
	if err != nil {
		t.Fatal(err)
	}
	box := cryptox.Open(dek)
	raw, _ := json.Marshal(model.IdentitySecret{Username: "alice", Password: "s3cret"})
	blob, err := box.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := secretUsername(box, blob); got != "alice" {
		t.Fatalf("username = %q", got)
	}
}

func TestKeepExistingInline(t *testing.T) {
	if !keepExistingInline(map[string]any{"label": "renamed"}, true) {
		t.Fatal("label-only patch should keep the one-time secret")
	}
	if keepExistingInline(map[string]any{"identity_id": "id-1"}, true) {
		t.Fatal("setting an identity should replace the one-time secret")
	}
	if keepExistingInline(map[string]any{"use_inline_identity": false, "identity_id": "id-1"}, true) {
		t.Fatal("explicit use_inline_identity should replace the one-time secret")
	}
	if keepExistingInline(map[string]any{"identity_id": "id-1"}, false) {
		t.Fatal("host without a one-time secret has nothing to keep")
	}
}
