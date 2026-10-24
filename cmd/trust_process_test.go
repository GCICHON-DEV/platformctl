package cmd

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestTemplateTrustProcessLifecycle(t *testing.T) {
	root := setupCLITemplate(t)
	data, err := os.ReadFile(filepath.Join(root, "template/platform.template.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var content atomic.Value
	content.Store(string(data))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, content.Load().(string)) }))
	defer server.Close()
	ca := filepath.Join(root, "synthetic-ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), "template:\n  source: "+server.URL+"/manifest.yaml\nvalues:\n  project_name: synthetic\n")
	run := func(want int, args ...string) {
		t.Helper()
		command := workflowProcess(t, root, append(args, "--json")...)
		command.Env = append(command.Env, "PLATFORMCTL_TEST_CA="+ca)
		out, err := command.CombinedOutput()
		if want == 0 && err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		if want != 0 && err == nil {
			t.Fatalf("%v unexpectedly passed", args)
		}
		var doc interface{}
		if json.Unmarshal(out, &doc) != nil {
			t.Fatalf("%v invalid JSON: %s", args, out)
		}
	}
	run(1, "plan")
	run(0, "template", "lock")
	run(1, "template", "lock")
	run(0, "plan")
	statePath := filepath.Join(root, ".platformctl/state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	content.Store(string(data) + "\n# moved tag\n")
	run(1, "plan")
	after, _ := os.ReadFile(statePath)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("changed tag modified state")
	}
	run(0, "template", "update")
	run(0, "plan")
	server.Close()
	run(1, "plan")
	run(0, "plan", "--offline")
	run(1, "template", "update", "--offline")
}
