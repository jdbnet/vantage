package store

import (
	"path/filepath"
	"testing"

	"github.com/jdbnet/vantage/internal/model"
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

func hostLabels(hosts []model.Host) []string {
	out := make([]string, len(hosts))
	for i, h := range hosts {
		out[i] = h.Label
	}
	return out
}

func containsLabel(hosts []model.Host, label string) bool {
	for _, h := range hosts {
		if h.Label == label {
			return true
		}
	}
	return false
}

func TestBrowseSearchScopedToFolder(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	iid, err := s.CreateIdentity("root", "password", "blob", nil)
	if err != nil {
		t.Fatal(err)
	}
	mkHost := func(folder *string, label string, tags []string) {
		t.Helper()
		if _, err := s.CreateHost(HostWrite{
			FolderID:   folder,
			Label:      label,
			Hostname:   label + ".example",
			Port:       22,
			Protocol:   "ssh",
			IdentityID: &iid,
			Tags:       tags,
		}); err != nil {
			t.Fatal(err)
		}
	}

	prod, err := s.CreateFolder("prod", nil)
	if err != nil {
		t.Fatal(err)
	}
	db, err := s.CreateFolder("db", &prod)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateFolder("other", nil)
	if err != nil {
		t.Fatal(err)
	}

	mkHost(nil, "root-alpha", []string{"shared"})
	mkHost(&prod, "prod-alpha", nil)
	mkHost(&db, "db-alpha", []string{"shared"})
	mkHost(&other, "other-alpha", []string{"shared"})

	fromProd, err := s.Browse(&prod, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !fromProd.SearchActive || len(fromProd.Breadcrumb) != 1 || fromProd.Breadcrumb[0].ID != prod {
		t.Fatalf("prod search meta %+v", fromProd)
	}
	if len(fromProd.Hosts) != 2 || containsLabel(fromProd.Hosts, "root-alpha") || containsLabel(fromProd.Hosts, "other-alpha") {
		t.Fatalf("prod search hosts %v", hostLabels(fromProd.Hosts))
	}
	if !containsLabel(fromProd.Hosts, "prod-alpha") || !containsLabel(fromProd.Hosts, "db-alpha") {
		t.Fatalf("prod search missing descendants %v", hostLabels(fromProd.Hosts))
	}

	fromDB, err := s.Browse(&db, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(fromDB.Hosts) != 1 || fromDB.Hosts[0].Label != "db-alpha" || len(fromDB.Breadcrumb) != 2 {
		t.Fatalf("db search %+v", fromDB)
	}

	fromRoot, err := s.Browse(nil, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(fromRoot.Hosts) != 4 {
		t.Fatalf("root search hosts %v", hostLabels(fromRoot.Hosts))
	}

	tagged, err := s.Browse(&prod, "tag:shared")
	if err != nil {
		t.Fatal(err)
	}
	if len(tagged.Hosts) != 1 || tagged.Hosts[0].Label != "db-alpha" {
		t.Fatalf("scoped tag search %v", hostLabels(tagged.Hosts))
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

func TestApplyRemoteOpsHostBeforeFolder(t *testing.T) {
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

	fid, err := a.CreateFolder("prod", nil)
	if err != nil {
		t.Fatal(err)
	}
	hid, err := a.CreateHost(HostWrite{
		FolderID: &fid,
		Label:    "db1",
		Hostname: "10.0.0.1",
		Port:     22,
		Protocol: "ssh",
	})
	if err != nil {
		t.Fatal(err)
	}
	ops, _, err := a.ChangesSince(0)
	if err != nil {
		t.Fatal(err)
	}
	var reversed []model.ChangeOp
	for i := len(ops) - 1; i >= 0; i-- {
		reversed = append(reversed, ops[i])
	}
	if err := b.ApplyRemoteOps(reversed); err != nil {
		t.Fatal(err)
	}
	folders, err := b.ListFolders()
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].ID != fid {
		t.Fatalf("folders %+v", folders)
	}
	h, err := b.GetHost(hid)
	if err != nil {
		t.Fatal(err)
	}
	if h.FolderID == nil || *h.FolderID != fid {
		t.Fatalf("host folder %+v", h.FolderID)
	}
}

func TestBackfillChangeLogFolders(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	ts := now()
	fid := "01folderbackfill0000000000"
	_, err = a.DB().Exec(
		`INSERT INTO folders(id, parent_id, label, created_at, updated_at) VALUES(?,?,?,?,?)`,
		fid, nil, "orphaned", ts, ts,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.BackfillChangeLog(); err != nil {
		t.Fatal(err)
	}
	ops, _, err := a.ChangesSince(0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range ops {
		if op.Entity == "folder" && op.EntityID == fid {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected backfilled folder op, got %+v", ops)
	}

	b, err := Open(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.ApplyRemoteOps(ops); err != nil {
		t.Fatal(err)
	}
	folders, err := b.ListFolders()
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Label != "orphaned" {
		t.Fatalf("folders %+v", folders)
	}
}

func TestChangesSinceLimit(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "vantage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 5; i++ {
		if _, err := s.CreateSnippet("s"+string(rune('a'+i)), "echo"); err != nil {
			t.Fatal(err)
		}
	}
	page1, head1, err := s.ChangesSinceLimit(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 %d", len(page1))
	}
	page2, _, err := s.ChangesSinceLimit(head1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) < 3 {
		t.Fatalf("page2 %d", len(page2))
	}
}

func TestNormalizeAccentColor(t *testing.T) {
	got, ok := NormalizeAccentColor("#1EBE8A")
	if !ok || got != "#1ebe8a" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	got, ok = NormalizeAccentColor("aabbcc")
	if !ok || got != "#aabbcc" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := NormalizeAccentColor("mint"); ok {
		t.Fatal("expected invalid")
	}
	if _, ok := NormalizeAccentColor("#fff"); ok {
		t.Fatal("expected 6-digit only")
	}
}
