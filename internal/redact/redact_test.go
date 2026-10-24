package redact

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanariesFlagsURLsJSONAndChunks(t *testing.T) {
	const secret = "fixture-canary-91!\"x"
	r := New(secret)
	for _, input := range []string{secret, `--password "` + secret + `"`, `https://user:password@example.invalid/path?token=canary`, `{"token":"canary"}`} {
		if got := r.Text(input); strings.Contains(got, secret) || strings.Contains(got, "password@") || strings.Contains(got, `"canary"`) {
			t.Fatalf("leak: %q", got)
		}
	}
	var out bytes.Buffer
	stream := NewStream(&out, r)
	for _, part := range []string{"prefix fixture-", "canary-91!\"", "x suffix\ntrailing ", secret} {
		if _, err := stream.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), "prefix "+Marker+" suffix") {
		t.Fatal(out.String())
	}
	encoded, _ := json.Marshal(map[string]interface{}{"ok": true, "credentials": []string{"still diagnostic"}, "token": "not-known", "message": secret, "count": 3})
	clean, err := r.JSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if json.Unmarshal(clean, &doc) != nil || doc["ok"] != true || doc["count"] != float64(3) || doc["credentials"] == Marker || strings.Contains(string(clean), "not-known") {
		t.Fatal(string(clean))
	}
}
func TestOversizedDiagnosticLineIsDiscarded(t *testing.T) {
	var out bytes.Buffer
	s := NewStream(&out, New("boundary-canary"))
	s.Write([]byte(strings.Repeat("a", MaxLine-5)))
	s.Write([]byte("boundary-canary"))
	s.Write([]byte(" trailing\nnext\n"))
	s.Flush()
	if strings.Contains(out.String(), "canary") || strings.Contains(out.String(), "aaaa") || !strings.HasSuffix(out.String(), "next\n") {
		t.Fatal(out.String())
	}
}
func TestRecognizedArgumentsBecomeOutputCanaries(t *testing.T) {
	r := Command([]string{"--password", "synthetic-arg-secret", "https://u:synthetic-url-secret@example.invalid", `{"access_token":"synthetic-json-secret"}`})
	for _, secret := range []string{"synthetic-arg-secret", "synthetic-url-secret", "synthetic-json-secret"} {
		if strings.Contains(r.Text("error: "+secret), secret) {
			t.Fatal("argument canary leaked")
		}
	}
}
