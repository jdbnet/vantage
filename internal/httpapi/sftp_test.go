package httpapi

import "testing"

func TestSftpListPathKeepsExplicit(t *testing.T) {
	p, err := sftpListPath(nil, "/var/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if p != "/var/tmp" {
		t.Fatalf("path %q", p)
	}
}
