package store

import (
	"path/filepath"
	"testing"
)

func TestFolderHostIdentityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	fid, err := s.CreateFolder("prod", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateFolder("db", &fid)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := s.ListFolders()
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 {
		t.Fatalf("folders=%d", len(folders))
	}

	iid, err := s.CreateIdentity("root", "password", "blob", nil)
	if err != nil {
		t.Fatal(err)
	}
	hid, err := s.CreateHost(HostWrite{
		FolderID:   &child,
		Label:      "db1",
		Hostname:   "10.0.0.1",
		Port:       22,
		Protocol:   "ssh",
		IdentityID: &iid,
		Tags:       []string{"prod", "db"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.GetHost(hid)
	if err != nil {
		t.Fatal(err)
	}
	if h.Label != "db1" || h.IdentityLabel != "root" {
		t.Fatalf("host %+v", h)
	}
	if len(h.Tags) != 2 {
		t.Fatalf("tags %v", h.Tags)
	}

	br, err := s.Browse(&child, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(br.Hosts) != 1 || len(br.Breadcrumb) != 2 {
		t.Fatalf("browse hosts=%d crumb=%d", len(br.Hosts), len(br.Breadcrumb))
	}

	tagged, err := s.Browse(nil, "tag:prod")
	if err != nil {
		t.Fatal(err)
	}
	if !tagged.SearchActive || len(tagged.Hosts) != 1 {
		t.Fatalf("tag search %+v", tagged)
	}

	if err := s.DeleteIdentity(iid); err == nil {
		t.Fatal("expected conflict deleting in-use identity")
	}
	if err := s.DeleteHost(hid); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteIdentity(iid); err != nil {
		t.Fatal(err)
	}
}

func TestChangeLogAndLWW(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	id, err := a.CreateSnippet("hello", "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	ops, _, err := a.ChangesSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 {
		t.Fatal("expected ops")
	}
	for _, op := range ops {
		if err := b.ApplyRemoteOp(op); err != nil {
			t.Fatal(err)
		}
	}
	snips, err := b.ListSnippets()
	if err != nil {
		t.Fatal(err)
	}
	if len(snips) != 1 || snips[0].ID != id {
		t.Fatalf("snips %+v", snips)
	}

	label := "newer"
	if err := b.UpdateSnippet(id, &label, nil); err != nil {
		t.Fatal(err)
	}
	bops, _, err := b.ChangesSince(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range bops {
		if err := a.ApplyRemoteOp(op); err != nil {
			t.Fatal(err)
		}
	}
	as, _ := a.ListSnippets()
	if as[0].Label != "newer" {
		t.Fatalf("lww failed: %+v", as)
	}
}
