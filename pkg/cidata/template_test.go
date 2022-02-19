package cidata

import (
	"io"
	"testing"
)

func TestTemplate(t *testing.T) {
	args := TemplateArgs{
		Name: "default",
		User: "foo",
		UID:  501,
		SSHPubKeys: []string{
			"ssh-rsa dummy foo@example.com",
		},
		Mounts: []string{
			"/Users/dummy",
			"/Users/dummy/lima",
		},
	}
	layout, _ := ExecuteTemplate(args)
	for _, f := range layout {
		t.Logf("=== %q ===", f.Path)
		b, _ := io.ReadAll(f.Reader)
		t.Log(string(b))
	}
}
