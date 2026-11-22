package httpapi

import "testing"

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
