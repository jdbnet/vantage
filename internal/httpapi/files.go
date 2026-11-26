package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jdbnet/vantage/internal/idgen"
	"github.com/pkg/sftp"
)

const transferKeep = 3 * time.Minute

// transferGate, when set, is waited on once at the start of a job.
// Tests use it to prove a job outlives the POST and stops on cancel.
var transferGate <-chan struct{}

type fileEnd struct {
	System string `json:"system"`
	ConnID string `json:"conn_id"`
	Path   string `json:"path"`
}

type transferJob struct {
	ID       string    `json:"id"`
	State    string    `json:"state"`
	Bytes    int64     `json:"bytes"`
	Total    int64     `json:"total"`
	Error    string    `json:"error,omitempty"`
	Conflict string    `json:"conflict,omitempty"`
	Policy   string    `json:"policy,omitempty"`
	ApplyAll bool      `json:"apply_all"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	Sources  []fileEnd `json:"sources"`
	Dest     fileEnd   `json:"dest"`
	srcConns []string
	dstConn  string
	ctx      context.Context
	cancel   context.CancelFunc
	decide   chan string
	mu       sync.Mutex
}

type transferBook struct {
	mu   sync.Mutex
	jobs map[string]*transferJob
}

func newTransferBook() *transferBook {
	return &transferBook{jobs: map[string]*transferJob{}}
}

func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sources []fileEnd `json:"sources"`
		Dest    fileEnd   `json:"dest"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if len(body.Sources) == 0 {
		writeErr(w, 400, "sources required")
		return
	}
	job, err := s.transfers.start(s, body.Sources, body.Dest)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": job.ID})
}

func (s *Server) handleListTransfers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"transfers": s.transfers.list()})
}

func (s *Server) handleCancelTransfer(w http.ResponseWriter, r *http.Request) {
	if !s.transfers.cancel(r.PathValue("id")) {
		writeErr(w, 404, "unknown transfer")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleResolveTransfer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action   string `json:"action"`
		ApplyAll bool   `json:"apply_all"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := s.transfers.resolve(r.PathValue("id"), body.Action, body.ApplyAll); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleCancelSessionTransfers(w http.ResponseWriter, r *http.Request) {
	s.cancelTransfers(r.PathValue("cid"))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) cancelTransfers(connID string) {
	if s.transfers == nil || connID == "" {
		return
	}
	s.transfers.cancelConn(connID)
}

func (b *transferBook) start(s *Server, sources []fileEnd, dest fileEnd) (*transferJob, error) {
	for i := range sources {
		if err := checkEnd(&sources[i]); err != nil {
			return nil, err
		}
	}
	if err := checkEnd(&dest); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &transferJob{
		ID:      idgen.New(),
		State:   "running",
		Started: time.Now(),
		Sources: sources,
		Dest:    dest,
		ctx:     ctx,
		cancel:  cancel,
		decide:  make(chan string, 1),
	}
	for _, src := range sources {
		if src.System == "sftp" {
			job.srcConns = append(job.srcConns, src.ConnID)
		}
		if nested(dest, src) {
			cancel()
			return nil, errors.New("destination is inside a source")
		}
	}
	if dest.System == "sftp" {
		job.dstConn = dest.ConnID
	}
	b.mu.Lock()
	b.jobs[job.ID] = job
	b.mu.Unlock()
	go job.run(s)
	return job, nil
}

func checkEnd(e *fileEnd) error {
	switch e.System {
	case "local":
		p, err := cleanLocalPath(e.Path)
		if err != nil {
			return err
		}
		e.Path = p
		return nil
	case "sftp":
		if strings.TrimSpace(e.ConnID) == "" {
			return errors.New("sftp connection required")
		}
		if strings.TrimSpace(e.Path) == "" {
			return nil
		}
		p, err := sanitizePath(e.Path)
		if err != nil {
			return err
		}
		e.Path = p
		return nil
	default:
		return errors.New("unknown system")
	}
}

func (b *transferBook) list() []transferJob {
	b.prune()
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]transferJob, 0, len(b.jobs))
	for _, j := range b.jobs {
		j.mu.Lock()
		out = append(out, transferJob{
			ID: j.ID, State: j.State, Bytes: j.Bytes, Total: j.Total, Error: j.Error,
			Conflict: j.Conflict, Policy: j.Policy, ApplyAll: j.ApplyAll,
			Started: j.Started, Finished: j.Finished, Sources: j.Sources, Dest: j.Dest,
		})
		j.mu.Unlock()
	}
	return out
}

func (b *transferBook) prune() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for id, j := range b.jobs {
		j.mu.Lock()
		done := !j.Finished.IsZero() && now.Sub(j.Finished) >= transferKeep
		j.mu.Unlock()
		if done {
			delete(b.jobs, id)
		}
	}
}

func (b *transferBook) cancel(id string) bool {
	b.mu.Lock()
	j := b.jobs[id]
	b.mu.Unlock()
	if j == nil {
		return false
	}
	j.stop("cancelled")
	return true
}

func (b *transferBook) cancelConn(connID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, j := range b.jobs {
		j.mu.Lock()
		hit := j.dstConn == connID || containsConn(j.srcConns, connID)
		state := j.State
		j.mu.Unlock()
		if hit && (state == "running" || state == "waiting") {
			j.stop("cancelled")
		}
	}
}

func (b *transferBook) resolve(id, action string, all bool) error {
	switch action {
	case "overwrite", "skip", "rename":
	default:
		return errors.New("action must be overwrite, skip, or rename")
	}
	b.mu.Lock()
	j := b.jobs[id]
	b.mu.Unlock()
	if j == nil {
		return errors.New("unknown transfer")
	}
	j.mu.Lock()
	if all {
		j.ApplyAll = true
		j.Policy = action
	}
	waiting := j.State == "waiting"
	j.mu.Unlock()
	if !waiting {
		if all {
			return nil
		}
		return errors.New("transfer is not waiting")
	}
	select {
	case j.decide <- action:
		return nil
	default:
		return errors.New("transfer is not waiting")
	}
}

func (j *transferJob) stop(state string) {
	j.mu.Lock()
	if j.State == "done" || j.State == "failed" || j.State == "cancelled" {
		j.mu.Unlock()
		return
	}
	j.State = state
	j.Finished = time.Now()
	j.mu.Unlock()
	j.cancel()
}

func (j *transferJob) run(s *Server) {
	err := j.copyAll(s)
	j.mu.Lock()
	terminal := j.State == "cancelled" || j.State == "failed" || j.State == "done"
	j.mu.Unlock()
	if terminal {
		return
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			j.stop("cancelled")
			return
		}
		j.mu.Lock()
		j.State = "failed"
		j.Error = err.Error()
		j.Finished = time.Now()
		j.mu.Unlock()
		return
	}
	j.mu.Lock()
	j.State = "done"
	j.Finished = time.Now()
	j.Conflict = ""
	j.mu.Unlock()
}

func (j *transferJob) copyAll(s *Server) error {
	if err := j.waitGate(); err != nil {
		return err
	}
	if err := fillHome(s, &j.Dest); err != nil {
		return err
	}
	for i := range j.Sources {
		if err := fillHome(s, &j.Sources[i]); err != nil {
			return err
		}
		if nested(j.Dest, j.Sources[i]) {
			return errors.New("destination is inside a source")
		}
	}
	var files []copyItem
	var total int64
	for _, src := range j.Sources {
		walked, err := walkEnd(s, src)
		if err != nil {
			return err
		}
		files = append(files, walked...)
		for _, it := range walked {
			if !it.dir {
				total += it.size
			}
		}
	}
	j.mu.Lock()
	j.Total = total
	j.mu.Unlock()
	for _, it := range files {
		if err := j.ctx.Err(); err != nil {
			return err
		}
		if it.dir {
			if err := mkdirEnd(s, j.Dest, it.destRel); err != nil {
				return err
			}
			continue
		}
		destPath, err := j.destPath(s, it)
		if err != nil {
			return err
		}
		if destPath == "" {
			j.addBytes(it.size)
			continue
		}
		if err := j.copyFile(s, it, destPath); err != nil {
			return err
		}
	}
	return nil
}

func fillHome(s *Server, e *fileEnd) error {
	if e.System != "sftp" || strings.TrimSpace(e.Path) != "" {
		return nil
	}
	c, err := s.sftpFrom(e.ConnID)
	if err != nil {
		return err
	}
	wd, err := c.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		e.Path = "/"
		return nil
	}
	p, err := sanitizePath(wd)
	if err != nil {
		e.Path = "/"
		return nil
	}
	e.Path = p
	return nil
}

func (j *transferJob) waitGate() error {
	if transferGate == nil {
		return nil
	}
	select {
	case <-transferGate:
		return nil
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
}

func (j *transferJob) addBytes(n int64) {
	j.mu.Lock()
	j.Bytes += n
	j.mu.Unlock()
}

type copyItem struct {
	src     fileEnd
	rel     string
	base    string
	destRel string
	size    int64
	dir     bool
}

func walkEnd(s *Server, src fileEnd) ([]copyItem, error) {
	info, err := statEnd(s, src)
	if err != nil {
		return nil, err
	}
	base := baseName(src)
	root := copyItem{src: src, base: base, destRel: base, size: info.Size(), dir: info.IsDir()}
	if !info.IsDir() {
		return []copyItem{root}, nil
	}
	out := []copyItem{root}
	kids, err := walkChildren(s, src, base)
	if err != nil {
		return nil, err
	}
	return append(out, kids...), nil
}

func walkChildren(s *Server, src fileEnd, destPrefix string) ([]copyItem, error) {
	entries, err := readEnd(s, src)
	if err != nil {
		return nil, err
	}
	var out []copyItem
	for _, e := range entries {
		child := src
		child.Path = joinEnd(src, e.Name())
		rel := destPrefix + "/" + e.Name()
		item := copyItem{src: child, base: baseName(src), destRel: rel, size: e.Size(), dir: e.IsDir()}
		out = append(out, item)
		if e.IsDir() {
			kids, err := walkChildren(s, child, rel)
			if err != nil {
				return nil, err
			}
			out = append(out, kids...)
		}
	}
	return out, nil
}

func (j *transferJob) destPath(s *Server, it copyItem) (string, error) {
	dest := j.Dest
	dest.Path = joinEnd(j.Dest, it.destRel)
	_, err := statEnd(s, dest)
	if err != nil {
		if os.IsNotExist(err) {
			return dest.Path, nil
		}
		return "", err
	}
	action, err := j.conflictAction(dest.Path)
	if err != nil {
		return "", err
	}
	switch action {
	case "skip":
		return "", nil
	case "overwrite":
		return dest.Path, nil
	case "rename":
		return uniqueEnd(s, j.Dest, it.destRel)
	default:
		return "", errors.New("unknown conflict action")
	}
}

func (j *transferJob) conflictAction(path string) (string, error) {
	j.mu.Lock()
	policy := j.Policy
	j.mu.Unlock()
	if policy != "" {
		return policy, nil
	}
	j.mu.Lock()
	j.State = "waiting"
	j.Conflict = path
	j.mu.Unlock()
	select {
	case action := <-j.decide:
		j.mu.Lock()
		if j.ApplyAll {
			j.Policy = action
		}
		if j.State == "waiting" {
			j.State = "running"
		}
		j.Conflict = ""
		j.mu.Unlock()
		return action, nil
	case <-j.ctx.Done():
		return "", j.ctx.Err()
	}
}

func (j *transferJob) copyFile(s *Server, it copyItem, destPath string) error {
	rc, err := openEnd(s, it.src)
	if err != nil {
		return err
	}
	defer rc.Close()
	wc, err := createEnd(s, j.Dest.System, j.Dest.ConnID, destPath)
	if err != nil {
		return err
	}
	defer wc.Close()
	_, err = io.Copy(progressWriter{w: wc, j: j}, ctxReader{ctx: j.ctx, r: rc})
	return err
}

type progressWriter struct {
	w io.Writer
	j *transferJob
}

func (p progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.j.addBytes(int64(n))
	}
	return n, err
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func baseName(e fileEnd) string {
	if e.System == "local" {
		return filepath.Base(e.Path)
	}
	return path.Base(e.Path)
}

func joinEnd(dir fileEnd, name string) string {
	if dir.System == "local" {
		return filepath.Join(dir.Path, filepath.FromSlash(name))
	}
	return path.Join(dir.Path, name)
}

func statEnd(s *Server, e fileEnd) (os.FileInfo, error) {
	if e.System == "local" {
		return os.Stat(e.Path)
	}
	c, err := s.sftpFrom(e.ConnID)
	if err != nil {
		return nil, err
	}
	return c.Stat(e.Path)
}

func readEnd(s *Server, e fileEnd) ([]os.FileInfo, error) {
	if e.System == "local" {
		des, err := os.ReadDir(e.Path)
		if err != nil {
			return nil, err
		}
		out := make([]os.FileInfo, 0, len(des))
		for _, d := range des {
			info, err := d.Info()
			if err != nil {
				return nil, err
			}
			out = append(out, info)
		}
		return out, nil
	}
	c, err := s.sftpFrom(e.ConnID)
	if err != nil {
		return nil, err
	}
	return c.ReadDir(e.Path)
}

func mkdirEnd(s *Server, dest fileEnd, rel string) error {
	p := joinEnd(dest, rel)
	if dest.System == "local" {
		return os.MkdirAll(p, 0o755)
	}
	c, err := s.sftpFrom(dest.ConnID)
	if err != nil {
		return err
	}
	return c.MkdirAll(p)
}

func openEnd(s *Server, e fileEnd) (io.ReadCloser, error) {
	if e.System == "local" {
		return os.Open(e.Path)
	}
	c, err := s.sftpFrom(e.ConnID)
	if err != nil {
		return nil, err
	}
	return c.Open(e.Path)
}

func createEnd(s *Server, system, connID, p string) (io.WriteCloser, error) {
	if system == "local" {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		return os.Create(p)
	}
	c, err := s.sftpFrom(connID)
	if err != nil {
		return nil, err
	}
	return c.Create(p)
}

func uniqueEnd(s *Server, dest fileEnd, rel string) (string, error) {
	dir := path.Dir(rel)
	name := path.Base(rel)
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if dir == "." {
		dir = ""
	}
	for i := 1; i < 1000; i++ {
		next := stem + " (" + itoa(i) + ")" + ext
		relNext := next
		if dir != "" {
			relNext = dir + "/" + next
		}
		p := joinEnd(dest, relNext)
		_, err := statEnd(s, fileEnd{System: dest.System, ConnID: dest.ConnID, Path: p})
		if os.IsNotExist(err) {
			return p, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not find a free name")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func containsConn(ids []string, id string) bool {
	for _, c := range ids {
		if c == id {
			return true
		}
	}
	return false
}

func nested(dest, src fileEnd) bool {
	if dest.System != src.System || dest.ConnID != src.ConnID {
		return false
	}
	d := dest.Path
	s := src.Path
	if dest.System == "local" {
		d = strings.ToLower(filepath.Clean(d))
		s = strings.ToLower(filepath.Clean(s))
		if d == s {
			return true
		}
		sep := string(os.PathSeparator)
		return strings.HasPrefix(d, s+sep)
	}
	if d == s {
		return true
	}
	return strings.HasPrefix(d, strings.TrimRight(s, "/")+"/")
}

func (s *Server) sftpFrom(connID string) (*sftp.Client, error) {
	if s.d.Conns == nil {
		return nil, errors.New("session ended")
	}
	sess := s.d.Conns.Get(connID)
	if sess == nil {
		return nil, errors.New("session ended")
	}
	return sess.GetSFTP()
}
