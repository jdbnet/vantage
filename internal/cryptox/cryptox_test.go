package cryptox

import "testing"

func TestEncryptRoundTrip(t *testing.T) {
	dek, err := NewDEK()
	if err != nil {
		t.Fatal(err)
	}
	box := Open(dek)
	ct, err := box.Encrypt("secret-value")
	if err != nil {
		t.Fatal(err)
	}
	got, err := box.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret-value" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapDEK(t *testing.T) {
	dek, err := NewDEK()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapDEK("hunter2", dek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapDEK("hunter2", wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dek) {
		t.Fatal("dek mismatch")
	}
	if _, err := UnwrapDEK("wrong", wrapped); err == nil {
		t.Fatal("expected error")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("pw", h) {
		t.Fatal("verify failed")
	}
	if VerifyPassword("no", h) {
		t.Fatal("false positive")
	}
}
