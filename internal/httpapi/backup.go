package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/jdbnet/vantage/internal/model"
	"github.com/jdbnet/vantage/internal/store"
)

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if s.d.Box() == nil {
		writeErr(w, http.StatusServiceUnavailable, "vault locked")
		return
	}
	snap, err := s.d.Store.Snapshot()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.rewriteSnapshotSecrets(snap, true)
	tags, err := s.d.Store.ListTagRecords()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	snap["tags"] = tags
	snap["exported_at"] = time.Now().UTC().Format(time.RFC3339)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="vantage-backup.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snap)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	box := s.d.Box()
	if box == nil {
		writeErr(w, http.StatusServiceUnavailable, "vault locked")
		return
	}
	var snap map[string]any
	if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	s.rewriteSnapshotSecrets(snap, false)
	n, err := importSnapshot(s.d.Store, snap)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "applied": n})
}

func importSnapshot(st *store.Store, snap map[string]any) (int, error) {
	applied := 0
	folders := asMapSlice(snap["folders"])
	sortFoldersParentsFirst(folders)
	for _, m := range folders {
		if err := applyImport(st, "folder", m); err != nil {
			return applied, err
		}
		applied++
	}
	for _, m := range asMapSlice(snap["identities"]) {
		if err := applyImport(st, "identity", m); err != nil {
			return applied, err
		}
		applied++
	}
	for _, m := range tagMaps(snap["tags"]) {
		if err := applyImport(st, "tag", m); err != nil {
			return applied, err
		}
		applied++
	}
	for _, m := range asMapSlice(snap["hosts"]) {
		if err := applyImport(st, "host", m); err != nil {
			return applied, err
		}
		applied++
	}
	for _, m := range asMapSlice(snap["snippets"]) {
		if err := applyImport(st, "snippet", m); err != nil {
			return applied, err
		}
		applied++
	}
	for _, m := range asMapSlice(snap["known_hosts"]) {
		if err := applyImport(st, "known_host", m); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func applyImport(st *store.Store, entity string, m map[string]any) error {
	id := strMap(m, "id")
	if id == "" {
		id = idgen.New()
		m["id"] = id
	}
	updated := strMap(m, "updated_at")
	if updated == "" {
		updated = time.Now().UTC().Format(time.RFC3339Nano)
		m["updated_at"] = updated
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return st.ImportApply(model.ChangeOp{
		Entity:    entity,
		EntityID:  id,
		Op:        "upsert",
		UpdatedAt: updated,
		Origin:    "import",
		Payload:   raw,
	})
}

func asMapSlice(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func tagMaps(v any) []map[string]any {
	if ms := asMapSlice(v); len(ms) > 0 {
		return ms
	}
	switch t := v.(type) {
	case []string:
		out := make([]map[string]any, 0, len(t))
		for _, name := range t {
			out = append(out, map[string]any{"id": idgen.New(), "name": name})
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, map[string]any{"id": idgen.New(), "name": s})
			}
		}
		return out
	default:
		return nil
	}
}

func strMap(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k]; ok && v != nil {
		return asString(v)
	}
	return ""
}

func sortFoldersParentsFirst(folders []map[string]any) {
	sort.SliceStable(folders, func(i, j int) bool {
		return folderDepth(folders[i], folders) < folderDepth(folders[j], folders)
	})
}

func folderDepth(f map[string]any, all []map[string]any) int {
	byID := map[string]map[string]any{}
	for _, x := range all {
		byID[strMap(x, "id")] = x
	}
	depth := 0
	cur := strMap(f, "parent_id")
	seen := map[string]struct{}{}
	for cur != "" {
		if _, ok := seen[cur]; ok {
			break
		}
		seen[cur] = struct{}{}
		parent, ok := byID[cur]
		if !ok {
			break
		}
		depth++
		cur = strMap(parent, "parent_id")
	}
	return depth
}
