package apperror

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRenderHumanReadableError(t *testing.T) {
	err := WithRemediation(
		WithContext(
			Wrap(errors.New("boom"), CategoryExecution, "PLATFORMCTL_TEST", "test failed"),
			"step=apply",
		),
		"retry the command",
	)

	var out bytes.Buffer
	Render(&out, err, true, false)
	text := out.String()
	for _, want := range []string{"Error [PLATFORMCTL_TEST]: test failed", "Context: step=apply", "Fix: retry the command", "Cause: boom"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered error missing %q:\n%s", want, text)
		}
	}
}

func TestRenderJSONError(t *testing.T) {
	var out bytes.Buffer
	Render(&out, New(CategoryConfig, "PLATFORMCTL_JSON", "json failed"), false, true)
	if !strings.Contains(out.String(), `"ok":false`) && !strings.Contains(out.String(), `"ok": false`) {
		t.Fatalf("JSON error missing ok=false: %s", out.String())
	}
	if !strings.Contains(out.String(), `"PLATFORMCTL_JSON"`) {
		t.Fatalf("JSON error missing code: %s", out.String())
	}
}
