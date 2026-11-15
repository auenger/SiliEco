package daemon

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

func TestProjectFilesRootAndPagination(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 205; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.md", i)), []byte("hello"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := readProjectFiles(protocol.ProjectFilesRequest{Root: root, Operation: "list"})
	if err != nil || len(out.Entries) != 200 || out.NextOffset != 200 {
		t.Fatalf("first page: %#v %v", out, err)
	}
	out, err = readProjectFiles(protocol.ProjectFilesRequest{Root: root, Operation: "list", Offset: 200})
	if err != nil || len(out.Entries) != 5 || out.NextOffset != 0 {
		t.Fatalf("last page: %#v %v", out, err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", filepath.Join(outside, "secret"), "escape/secret"} {
		if _, err := readProjectFiles(protocol.ProjectFilesRequest{Root: root, Path: path, Operation: "preview"}); err == nil {
			t.Fatalf("allowed escape %s", path)
		}
	}
}

func TestProjectFilesLargeHTMLAndMarkdown(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"report.html", "report.md"} {
		original := strings.Repeat("你好 markdown <h1>preview</h1>\n", 90000)
		if err := os.WriteFile(filepath.Join(root, name), []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		var result strings.Builder
		offset := 0
		for {
			out, err := readProjectFiles(protocol.ProjectFilesRequest{Root: root, Path: name, Operation: "preview", Offset: offset})
			if err != nil {
				t.Fatal(err)
			}
			chunk, err := base64.StdEncoding.DecodeString(out.Content)
			if err != nil {
				t.Fatal(err)
			}
			if len(chunk) > 256<<10 {
				t.Fatal("unbounded chunk")
			}
			result.Write(chunk)
			if out.NextOffset == 0 {
				break
			}
			if out.NextOffset <= offset {
				t.Fatal("invalid continuation")
			}
			offset = out.NextOffset
		}
		if result.String() != original {
			t.Fatal("document truncated or corrupted")
		}
	}
}
