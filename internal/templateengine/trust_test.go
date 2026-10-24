package templateengine

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const trustManifest = `apiVersion: platformctl.io/v1beta1
metadata: {name: trusted, description: Synthetic trust test}
steps:
  apply: [{name: apply, command: echo}]
  destroy: [{name: destroy, command: echo}]
`

func TestTrustedSourceChangedTagOfflineAndAtomicUpdate(t *testing.T) {
	chdirForTest(t, t.TempDir())
	data := trustManifest
	status := 200
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); fmt.Fprint(w, data) }))
	defer server.Close()
	old := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = old }()
	source := TemplateSource{Source: server.URL + "/mutable-tag.yaml"}
	resolved, _ := ResolveTemplateSource(source)
	if _, err := readTrustedSource(resolved, false); err == nil {
		t.Fatal("unlocked source accepted")
	}
	lock, err := TrustSource(source, false)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(SourceLockPath)
	if _, err = TrustSource(source, false); err == nil {
		t.Fatal("implicit lock replacement")
	}
	if got, err := readTrustedSource(resolved, false); err != nil || string(got) != data {
		t.Fatalf("trusted online: %v", err)
	}
	data += "\n# changed tag\n"
	if _, err = readTrustedSource(resolved, false); err == nil {
		t.Fatal("changed tag accepted")
	}
	if got, err := readTrustedSource(resolved, true); err != nil || checksum(got) != lock.SHA256 {
		t.Fatal("explicit offline does not use approved bytes", err)
	}
	status = 404
	if _, err = readTrustedSource(resolved, false); err == nil {
		t.Fatal("silent fallback on404")
	}
	if _, err = TrustSource(source, true); err == nil {
		t.Fatal("failed update accepted")
	}
	after, _ := os.ReadFile(SourceLockPath)
	if !bytes.Equal(before, after) {
		t.Fatal("failed update changed lock")
	}
	status = 200
	if _, err = TrustSource(source, true); err != nil {
		t.Fatal(err)
	}
	if got, err := readTrustedSource(resolved, false); err != nil || string(got) != data {
		t.Fatal("explicit update failed", err)
	}
	server.Close()
	if _, err = readTrustedSource(resolved, false); err == nil {
		t.Fatal("network failure silently used cache")
	}
	if _, err = readTrustedSource(resolved, true); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(SourceLockPath)
	current = bytes.Replace(current, []byte(`"format": 1`), []byte(`"format": 2`), 1)
	os.WriteFile(SourceLockPath, current, 0600)
	if _, err = readTrustedSource(resolved, true); err == nil {
		t.Fatal("corrupt lock/cache accepted")
	}
}
func TestTrustedSourceLimitsAndNoDowngrade(t *testing.T) {
	chdirForTest(t, t.TempDir())
	mode := "large"
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch mode {
		case "large":
			fmt.Fprint(w, strings.Repeat("x", MaxTemplateBytes+1))
		case "redirect":
			http.Redirect(w, r, "/again", 302)
		case "downgrade":
			http.Redirect(w, r, "http://127.0.0.1:1/blocked", 302)
		}
	}))
	defer server.Close()
	old := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = old }()
	source, _ := ResolveTemplateSource(TemplateSource{Source: server.URL + "/template"})
	for _, next := range []string{"large", "redirect", "downgrade"} {
		mode = next
		calls = 0
		if _, err := downloadSource(source); err == nil {
			t.Fatal("unbounded download", next)
		}
		if next == "redirect" && calls != 4 {
			t.Fatalf("redirect requests=%d want4", calls)
		}
	}
}
