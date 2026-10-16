package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/offline_download/tool"
)

func TestSimpleHttpCustomFilename(t *testing.T) {
	previousConfig := conf.Conf
	conf.Conf = conf.DefaultConfig(t.TempDir())
	t.Cleanup(func() { conf.Conf = previousConfig })
	for _, test := range []struct {
		name, filename, disposition, want string
		wantError                         bool
	}{
		{name: "custom overrides header", filename: "custom.txt", disposition: `attachment; filename="server.txt"`, want: "custom.txt"},
		{name: "header fallback", disposition: `attachment; filename="server.txt"`, want: "server.txt"},
		{name: "URL fallback", want: "url.txt"},
		{name: "reject traversal", filename: "../escape.txt", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Disposition", test.disposition)
				_, _ = w.Write([]byte("download content"))
			}))
			defer server.Close()
			task := &tool.DownloadTask{Url: server.URL + "/url.txt", TempDir: t.TempDir(), Filename: test.filename}
			task.SetCtx(context.Background())
			err := (SimpleHttp{}).Run(task)
			if test.wantError {
				if err == nil {
					t.Fatal("expected an illegal filename error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(task.TempDir, test.want))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "download content" {
				t.Fatalf("content = %q", content)
			}
		})
	}
}
