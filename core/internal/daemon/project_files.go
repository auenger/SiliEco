package daemon

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// Project reads use an OS-enforced root, including during concurrent symlink changes.
func readProjectFiles(req protocol.ProjectFilesRequest) (protocol.ProjectFilesResponse, error) {
	out := protocol.ProjectFilesResponse{RequestID: req.RequestID, Entries: []protocol.ProjectFileEntry{}}
	if !filepath.IsAbs(req.Root) || req.Offset < 0 || (req.Operation == "list" && req.Offset > 100000) {
		return out, errors.New("invalid directory request")
	}
	path := req.Path
	if path == "" {
		path = "."
	}
	if !filepath.IsLocal(path) {
		return out, errors.New("path must stay inside the project directory")
	}
	root, err := os.OpenRoot(req.Root)
	if err != nil {
		return out, errors.New("project directory is unavailable")
	}
	defer root.Close()
	info, err := root.Stat(path)
	if err != nil {
		return out, errors.New("file is unavailable or outside the project directory")
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return out, errors.New("only directories and regular files are supported")
	}
	f, err := root.Open(path)
	if err != nil {
		return out, errors.New("cannot read project file")
	}
	defer f.Close()
	switch req.Operation {
	case "list":
		if !info.IsDir() {
			return out, errors.New("not a directory")
		}
		// Read one page, never recursively scan or sort an entire large directory.
		for remaining := req.Offset; remaining > 0; {
			n := remaining
			if n > 200 {
				n = 200
			}
			skipped, e := f.ReadDir(n)
			remaining -= len(skipped)
			if e == io.EOF {
				return out, nil
			}
			if e != nil {
				return out, e
			}
		}
		entries, err := f.ReadDir(201)
		if err != nil && err != io.EOF {
			return out, err
		}
		if len(entries) > 200 {
			entries = entries[:200]
			out.NextOffset = req.Offset + 200
		}
		for _, entry := range entries {
			// Do not expose symlinks or special devices as browsable entries.
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			i, e := entry.Info()
			if e != nil || (!i.IsDir() && !i.Mode().IsRegular()) {
				continue
			}
			out.Entries = append(out.Entries, protocol.ProjectFileEntry{Name: entry.Name(), Directory: i.IsDir(), Size: i.Size()})
		}
	case "preview":
		if info.IsDir() {
			return out, errors.New("select a file to preview")
		}
		// Transfer large documents in bounded chunks; Markdown/HTML have no total size cap.
		head := make([]byte, 512)
		n, _ := f.ReadAt(head, 0)
		mime := http.DetectContentType(head[:n])
		ext := strings.ToLower(filepath.Ext(path))
		switch {
		case ext == ".html" || ext == ".htm":
			mime = "text/html"
		case ext == ".md" || ext == ".markdown":
			mime = "text/markdown"
		case mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp":
		default:
			if strings.ContainsRune(string(head[:n]), 0) {
				return out, errors.New("this file type cannot be previewed")
			}
			mime = "text/plain"
		}
		if req.Offset > 0 {
			if _, err := f.Seek(int64(req.Offset), io.SeekStart); err != nil {
				return out, err
			}
		}
		content, err := io.ReadAll(io.LimitReader(f, 256<<10))
		if err != nil {
			return out, err
		}
		out.Mime = mime
		out.Content = base64.StdEncoding.EncodeToString(content)
		if int64(req.Offset+len(content)) < info.Size() {
			out.NextOffset = req.Offset + len(content)
		}

	default:
		return out, errors.New("unsupported file operation")
	}
	return out, nil
}

func (d *Daemon) handleProjectFiles(raw json.RawMessage, generation uint64) {
	var req protocol.ProjectFilesRequest
	if json.Unmarshal(raw, &req) != nil || req.RequestID == "" {
		return
	}
	d.projectFileMu.Lock()
	out, err := readProjectFiles(req)
	d.projectFileMu.Unlock()
	if err != nil {
		out.Error = err.Error()
	}
	payload, _ := json.Marshal(out)
	frame, _ := json.Marshal(protocol.Message{Type: protocol.EventProjectFilesResponse, Payload: payload})
	d.wsRPC.mu.Lock()
	defer d.wsRPC.mu.Unlock()
	if d.wsRPC.generation == generation && d.wsRPC.sendFrame != nil {
		_, _ = d.wsRPC.sendFrame(frame)
	}
}
