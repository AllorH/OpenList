package aria2

import (
	"reflect"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/offline_download/tool"
	"github.com/OpenListTeam/OpenList/v4/pkg/aria2/rpc"
)

type optionsClient struct {
	rpc.Client
	uris    []string
	options map[string]interface{}
}

func (c *optionsClient) AddURI(uris []string, options ...interface{}) (string, error) {
	c.uris = uris
	c.options = options[0].(map[string]interface{})
	return "options-test-gid", nil
}

func TestAddURLPassesOptionsAndPreservesTaskDirectory(t *testing.T) {
	client := &optionsClient{}
	a := &Aria2{client: client}
	options := map[string]interface{}{
		"out":    "custom.zip",
		"header": []string{"Referer: https://example.com/"},
		"dir":    "user-directory",
	}
	args := &tool.AddUrlArgs{Url: "https://example.com/file", TempDir: "task-directory", Options: options}
	gid, err := a.AddURL(args)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { notify.Signals.Delete(gid) })
	if !reflect.DeepEqual(client.uris, []string{args.Url}) {
		t.Fatalf("uris = %v", client.uris)
	}
	want := map[string]interface{}{
		"out": "custom.zip", "header": []string{"Referer: https://example.com/"}, "dir": args.TempDir,
	}
	if !reflect.DeepEqual(client.options, want) {
		t.Fatalf("options = %#v, want %#v", client.options, want)
	}
	if options["dir"] != "user-directory" {
		t.Fatal("AddURL mutated the caller's options")
	}
}
