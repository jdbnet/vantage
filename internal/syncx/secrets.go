package syncx

import (
	"encoding/json"

	"github.com/jdbnet/vantage/internal/cryptox"
	"github.com/jdbnet/vantage/internal/model"
)

// RewriteOp decrypts vault ciphertext on the way out and re-encrypts on the way in
// so each replica stores secrets under its own key.
func RewriteOp(box *cryptox.Box, op model.ChangeOp, outbound bool) model.ChangeOp {
	if box == nil || (op.Entity != "identity" && op.Entity != "host") {
		return op
	}
	var m map[string]any
	if json.Unmarshal(op.Payload, &m) != nil {
		return op
	}
	if op.Entity == "identity" {
		rewriteIdentMap(m, box, outbound)
	}
	if op.Entity == "host" {
		rewriteHostMap(m, box, outbound)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return op
	}
	op.Payload = b
	return op
}

// RewriteSnapshot rewrites identity and host secrets in a snapshot payload.
func RewriteSnapshot(box *cryptox.Box, snap map[string]any, outbound bool) {
	if box == nil || snap == nil {
		return
	}
	for _, m := range asMapSlice(snap["identities"]) {
		rewriteIdentMap(m, box, outbound)
	}
	for _, m := range asMapSlice(snap["hosts"]) {
		rewriteHostMap(m, box, outbound)
	}
}

func rewriteIdentMap(m map[string]any, box *cryptox.Box, outbound bool) {
	if m == nil {
		return
	}
	if outbound {
		if blob := mapString(m, "encrypted_blob"); blob != "" {
			if plain, err := box.Decrypt(blob); err == nil {
				m["secret"] = json.RawMessage(plain)
				delete(m, "encrypted_blob")
			}
		}
		if p := mapString(m, "encrypted_key_passphrase"); p != "" {
			if plain, err := box.Decrypt(p); err == nil {
				m["key_passphrase"] = plain
				delete(m, "encrypted_key_passphrase")
			}
		}
		return
	}
	if sec, ok := m["secret"]; ok {
		raw, _ := json.Marshal(sec)
		if blob, err := box.Encrypt(string(raw)); err == nil {
			m["encrypted_blob"] = blob
			delete(m, "secret")
		}
	}
	if kp, _ := m["key_passphrase"].(string); kp != "" {
		if enc, err := box.Encrypt(kp); err == nil {
			m["encrypted_key_passphrase"] = enc
			delete(m, "key_passphrase")
		}
	}
}

func rewriteHostMap(m map[string]any, box *cryptox.Box, outbound bool) {
	if m == nil {
		return
	}
	if outbound {
		if blob := mapString(m, "inline_identity_encrypted_blob"); blob != "" {
			if plain, err := box.Decrypt(blob); err == nil {
				m["inline_secret"] = json.RawMessage(plain)
				delete(m, "inline_identity_encrypted_blob")
			}
		}
		if p := mapString(m, "inline_identity_encrypted_key_passphrase"); p != "" {
			if plain, err := box.Decrypt(p); err == nil {
				m["inline_key_passphrase"] = plain
				delete(m, "inline_identity_encrypted_key_passphrase")
			}
		}
		return
	}
	if sec, ok := m["inline_secret"]; ok {
		raw, _ := json.Marshal(sec)
		if blob, err := box.Encrypt(string(raw)); err == nil {
			m["inline_identity_encrypted_blob"] = blob
			delete(m, "inline_secret")
		}
	}
	if kp, _ := m["inline_key_passphrase"].(string); kp != "" {
		if enc, err := box.Encrypt(kp); err == nil {
			m["inline_identity_encrypted_key_passphrase"] = enc
			delete(m, "inline_key_passphrase")
		}
	}
}

func mapString(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	}
	return ""
}
