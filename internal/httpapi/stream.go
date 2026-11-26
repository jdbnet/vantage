package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/jdbnet/vantage/internal/explorer"
)

func contentTypeOf(name string, ra io.ReaderAt) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	buf := make([]byte, 512)
	n, _ := ra.ReadAt(buf, 0)
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}

// serveStream writes a file with Content-Length and a single byte range.
// It uses ReaderAt and the known size so it does not seek the SFTP client,
// which can leave that client returning EOF on the next call.
func contentTypeName(name string) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// serveSized writes a file whose bytes come from open. length 0 on open means the whole file.
func serveSized(w http.ResponseWriter, r *http.Request, name string, size int64, open func(start, length int64) (io.ReadCloser, error)) {
	if size < 0 {
		size = 0
	}
	start, length := int64(0), size
	status := http.StatusOK
	if rh := strings.TrimSpace(r.Header.Get("Range")); rh != "" && size > 0 {
		s, n, err := parseByteRange(rh, size)
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, length = s, n
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
	}
	rc, err := open(start, length)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentTypeName(name))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)
	if r.Method == http.MethodHead || length == 0 {
		return
	}
	_, _ = io.Copy(w, rc)
}

func serveFull(w http.ResponseWriter, r *http.Request, name string, size int64, body io.Reader) {
	if size < 0 {
		size = 0
	}
	w.Header().Set("Content-Type", contentTypeName(name))
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, body)
}

func serveExplorer(w http.ResponseWriter, r *http.Request, name string, exp explorer.Explorer, filePath string) {
	info, err := exp.Stat(filePath)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if info.IsDir {
		writeErr(w, 400, "not a file")
		return
	}
	if rg, ok := exp.(explorer.Ranger); ok {
		serveSized(w, r, name, info.Size, func(start, length int64) (io.ReadCloser, error) {
			if start == 0 && (length == info.Size || length == 0) {
				return exp.ReadFile(filePath)
			}
			return rg.ReadRange(filePath, start, length)
		})
		return
	}
	rc, err := exp.ReadFile(filePath)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if ra, ok := rc.(io.ReaderAt); ok {
		defer rc.Close()
		serveStream(w, r, name, info.Size, ra)
		return
	}
	defer rc.Close()
	serveFull(w, r, name, info.Size, rc)
}

func serveStream(w http.ResponseWriter, r *http.Request, name string, size int64, ra io.ReaderAt) {
	if size < 0 {
		size = 0
	}
	start, length := int64(0), size
	status := http.StatusOK
	if rh := strings.TrimSpace(r.Header.Get("Range")); rh != "" && size > 0 {
		s, n, err := parseByteRange(rh, size)
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, length = s, n
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
	}
	w.Header().Set("Content-Type", contentTypeOf(name, ra))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)
	if r.Method == http.MethodHead || length == 0 {
		return
	}
	_, _ = io.Copy(w, io.NewSectionReader(ra, start, length))
}

func parseByteRange(h string, size int64) (start, length int64, err error) {
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, errors.New("invalid range")
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, errors.New("invalid range")
	}
	if left == "" {
		n, convErr := strconv.ParseInt(right, 10, 64)
		if convErr != nil || n <= 0 {
			return 0, 0, errors.New("invalid range")
		}
		if n > size {
			n = size
		}
		return size - n, n, nil
	}
	start, err = strconv.ParseInt(left, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, errors.New("invalid range")
	}
	end := size - 1
	if right != "" {
		end, err = strconv.ParseInt(right, 10, 64)
		if err != nil || end < start {
			return 0, 0, errors.New("invalid range")
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end - start + 1, nil
}
