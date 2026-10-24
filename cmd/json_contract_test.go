package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"platformctl/internal/apperror"
)

// Build main, not the test helper: exit codes and stdout/stderr are the actual
// public process boundary. Fixtures run only local echo/false commands.
func TestJSONBinaryContract(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "platformctl")
	build := exec.Command("go", "build", "-o", bin, "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	run := func(t *testing.T, root, input string, args ...string) (int, []byte, string) {
		t.Helper()
		command := exec.Command(bin, args...)
		command.Dir = root
		command.Env = append(os.Environ(), "HOME="+filepath.Join(root, ".home"))
		command.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		code := 0
		if err := command.Run(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		return code, stdout.Bytes(), stderr.String()
	}
	cases := []struct {
		name string
		args []string
		exit int
		code string
		key  string
	}{
		{"status", []string{"status", "--json"}, 0, "", "state"},
		{"global before", []string{"--json", "status"}, 0, "", "state"},
		{"quiet", []string{"status", "--quiet", "--json"}, 0, "", "state"},
		{"root help", []string{"--json"}, 0, "", "help"},
		{"help flag", []string{"status", "--help", "--json"}, 0, "", "help"},
		{"short help", []string{"status", "-h", "--json"}, 0, "", "help"},
		{"help command", []string{"help", "status", "--json"}, 0, "", "help"},
		{"unknown help", []string{"help", "missing", "--json"}, 2, "PLATFORMCTL_USAGE", ""},
		{"unknown command before flag", []string{"missing", "--json"}, 2, "PLATFORMCTL_USAGE", ""},
		{"unknown command after flag", []string{"--json", "missing"}, 2, "PLATFORMCTL_USAGE", ""},
		{"unknown flag before json", []string{"status", "--missing", "--json"}, 2, "PLATFORMCTL_USAGE", ""},
		{"flag from other command", []string{"status", "--project", "--json"}, 2, "PLATFORMCTL_USAGE", ""},
		{"invalid bool", []string{"status", "--json=invalid"}, 2, "PLATFORMCTL_USAGE", ""},
		{"unexpected argument", []string{"status", "extra", "--json"}, 2, "PLATFORMCTL_USAGE", ""},
		{"separator", []string{"status", "--json", "--", "--help"}, 2, "PLATFORMCTL_USAGE", ""},
		{"missing config", []string{"plan", "--json", "--verbose"}, 1, "PLATFORMCTL_CONFIG_LOAD", ""},
		{"missing flag value", []string{"init", "--json", "--template"}, 2, "PLATFORMCTL_USAGE", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, stderr := run(t, t.TempDir(), "", tc.args...)
			if code != tc.exit || stderr != "" {
				t.Fatalf("exit=%d stderr=%q out=%s", code, stderr, out)
			}
			doc := decodeDocument(t, out)
			if tc.code != "" {
				assertErrorCode(t, doc, tc.code)
			} else {
				if doc["ok"] != true || doc[tc.key] == nil {
					t.Fatalf("payload: %#v", doc)
				}
			}
		})
	}
	t.Run("mode values and separator", func(t *testing.T) {
		for _, args := range [][]string{{"status", "--json=false"}, {"status", "--", "--json"}, {"init", "--template", "--json"}, {"init", "--project", "--json"}, {"status", "--json", "--json=false"}} {
			_, out, _ := run(t, t.TempDir(), "", args...)
			if json.Valid(out) {
				t.Fatalf("flag value/separator interpreted as mode: %v: %s", args, out)
			}
		}
	})
	t.Run("init and preflight success", func(t *testing.T) {
		root := setupCLITemplate(t)
		if err := os.Remove(filepath.Join(root, "platform.yaml")); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := run(t, root, "", "init", "--template", "./template", "--project", "demo", "--json")
		doc := decodeDocument(t, out)
		if code != 0 || doc["ok"] != true || doc["config"] != "platform.yaml" || doc["template"] != "./template" || doc["preflight"] == nil || stderr != "" {
			t.Fatalf("init: %d %s %s", code, out, stderr)
		}
		for _, name := range []string{"preflight", "doctor"} {
			code, out, stderr = run(t, root, "", name, "--strict", "--json")
			if code != 0 || decodeDocument(t, out)["ok"] != true || stderr != "" {
				t.Fatalf("preflight: %d %s %s", code, out, stderr)
			}
		}
	})
	t.Run("warning strict and doctor", func(t *testing.T) {
		root := setupCLITemplate(t)
		path := filepath.Join(root, "template", "platform.template.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest := strings.Replace(string(data), "requirements:\n", "requirements:\n  credentials:\n    - name: synthetic-credential\n      command: false\n", 1)
		writeCLIFile(t, path, manifest)
		for _, name := range []string{"preflight", "doctor"} {
			code, out, stderr := run(t, root, "", name, "--json")
			doc := decodeDocument(t, out)
			if code != 0 || doc["ok"] != false || doc["credentials"] == nil || doc["template"] == nil || stderr != "" {
				t.Fatalf("warning: exit=%d doc=%#v stderr=%s", code, doc, stderr)
			}
			code, out, _ = run(t, root, "", name, "--strict", "--json")
			if code != 1 {
				t.Fatalf("strict exit=%d: %s", code, out)
			}
			assertErrorCode(t, decodeDocument(t, out), "PLATFORMCTL_CREDENTIAL_FAILED")
		}
		if err := os.Remove(filepath.Join(root, "platform.yaml")); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := run(t, root, "", "init", "--template", "./template", "--project", "demo", "--json")
		doc := decodeDocument(t, out)
		if code != 0 || doc["ok"] != false || doc["config"] != "platform.yaml" || doc["preflight"] == nil || stderr != "" {
			t.Fatalf("init warning exit=%d doc=%#v stderr=%s", code, doc, stderr)
		}
	})
	t.Run("lifecycle aliases cancellation and streams", func(t *testing.T) {
		root := setupCLITemplate(t)
		// Both child streams belong on stderr; no shell output may corrupt JSON.
		path := filepath.Join(root, "template", "platform.template.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest := strings.Replace(string(data), "command: echo\n      args: [\"apply\", \"{{ .Values.project_name }}\"]", "command: sh\n      args: [-c, 'echo child-out; echo child-err >&2']", 1)
		writeCLIFile(t, path, manifest)
		code, out, stderr := run(t, root, "", "plan", "--json")
		if code != 0 || decodeDocument(t, out)["plan"] == nil || stderr != "" {
			t.Fatalf("plan: %d %s %s", code, out, stderr)
		}
		before := approvalFiles(t, root)
		for _, phase := range []string{"apply", "destroy"} {
			code, out, _ := run(t, root, "", phase, "--json")
			if code != 1 {
				t.Fatalf("approval exit=%d: %s", code, out)
			}
			assertErrorCode(t, decodeDocument(t, out), "PLATFORMCTL_CONFIRMATION_REQUIRED")
			code, _, _ = run(t, root, "n\n", phase)
			if code != 0 {
				t.Fatalf("cancel exit=%d", code)
			}
			code, _, _ = run(t, root, "", phase)
			if code != 1 {
				t.Fatalf("EOF exit=%d", code)
			}
			if !reflect.DeepEqual(before, approvalFiles(t, root)) {
				t.Fatal("cancel/refusal changed managed files or state")
			}
		}
		code, out, stderr = run(t, root, "", "up", "--yes", "--json")
		doc := decodeDocument(t, out)
		if code != 0 || doc["phase"] != "apply" || doc["generated_files"] == nil || !strings.Contains(stderr, "child-out") || !strings.Contains(stderr, "child-err") {
			t.Fatalf("apply: %d %s %s", code, out, stderr)
		}
		for _, key := range []string{"warnings", "notes", "next_steps", "success_messages"} {
			if _, ok := doc[key]; !ok {
				t.Fatalf("lost apply field %s", key)
			}
		}
		code, out, stderr = run(t, root, "", "down", "--yes", "--json")
		if code != 0 || decodeDocument(t, out)["phase"] != "destroy" {
			t.Fatalf("destroy: %d %s %s", code, out, stderr)
		}
	})
	t.Run("workflow failure", func(t *testing.T) {
		root := setupCLITemplate(t)
		path := filepath.Join(root, "template", "platform.template.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeCLIFile(t, path, strings.Replace(string(data), "command: echo\n      args: [\"apply\", \"{{ .Values.project_name }}\"]", "command: sh\n      args: [-c, 'echo failed-child; exit 7']", 1))
		code, out, stderr := run(t, root, "", "apply", "--yes", "--json")
		doc := decodeDocument(t, out)
		if code != 1 || doc["ok"] != false || doc["error"] == nil || !strings.Contains(stderr, "failed-child") {
			t.Fatalf("failure: %d %s %s", code, out, stderr)
		}
	})
	t.Run("unwritable stdout", func(t *testing.T) {
		for _, args := range [][]string{{"status", "--json"}, {"status"}} {
			file, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(bin, args...)
			command.Dir = t.TempDir()
			command.Stdout = file
			var stderr bytes.Buffer
			command.Stderr = &stderr
			err = command.Run()
			file.Close()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("write failure falsely succeeded: %v", err)
			}
			if !strings.Contains(stderr.String(), "PLATFORMCTL_OUTPUT_WRITE") {
				t.Fatalf("write diagnostic: %s", stderr.String())
			}
		}
	})
}

func decodeDocument(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, data)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("more than one document: %v: %s", err, data)
	}
	return doc
}

func assertErrorCode(t *testing.T, doc map[string]interface{}, code string) {
	t.Helper()
	app, ok := doc["error"].(map[string]interface{})
	if doc["ok"] != false || !ok || app["code"] != code || app["category"] == nil || app["message"] == nil {
		t.Fatalf("error payload: %#v", doc)
	}
}

func TestJSONEncodingFailureAndRuntimeClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*cobra.Command, []string) error
		code string
	}{
		{"encode", func(cmd *cobra.Command, _ []string) error {
			if err := writeJSON(cmd.OutOrStdout(), map[string]bool{"ok": true}); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), make(chan int))
		}, "PLATFORMCTL_OUTPUT_ENCODE"},
		{"provider text", func(_ *cobra.Command, _ []string) error {
			return apperror.New(apperror.CategoryExecution, "PROVIDER_FAILED", "unknown command: provider returned usage")
		}, "PROVIDER_FAILED"},
		{"invalid result", func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), "not JSON")
			return err
		}, "PLATFORMCTL_OUTPUT_ENCODE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options = cliOptions{}
			t.Cleanup(func() { options = cliOptions{} })
			root := &cobra.Command{Use: "fixture", SilenceErrors: true, SilenceUsage: true, RunE: tc.run}
			root.PersistentFlags().BoolVar(&options.JSON, "json", false, "")
			var out, stderr bytes.Buffer
			code := runCLI(root, []string{"--json"}, strings.NewReader(""), &out, &stderr)
			if code != 1 || stderr.Len() != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			assertErrorCode(t, decodeDocument(t, out.Bytes()), tc.code)
		})
	}
}

type shortOutput struct{}

func (shortOutput) Write(data []byte) (int, error) { return len(data) / 2, nil }
func TestJSONWriterErrors(t *testing.T) {
	err := writeJSON(shortOutput{}, map[string]bool{"ok": true})
	if !errors.Is(err, io.ErrShortWrite) || apperror.Normalize(err).Code != "PLATFORMCTL_OUTPUT_WRITE" {
		t.Fatalf("short write: %v", err)
	}
	var out bytes.Buffer
	if err := writeJSON(&out, make(chan int)); err == nil || out.Len() != 0 {
		t.Fatalf("encoding left partial document: %v %s", err, out.String())
	}
}

func TestCLIShortWriteReporting(t *testing.T) {
	options = cliOptions{}
	t.Cleanup(func() { options = cliOptions{} })
	root := &cobra.Command{Use: "fixture", RunE: func(cmd *cobra.Command, _ []string) error {
		return writeJSON(cmd.OutOrStdout(), map[string]bool{"ok": true})
	}}
	root.PersistentFlags().BoolVar(&options.JSON, "json", false, "")
	var stderr bytes.Buffer
	if code := runCLI(root, []string{"--json"}, strings.NewReader(""), shortOutput{}, &stderr); code != 1 {
		t.Fatalf("short output returned %d", code)
	}
	if !strings.Contains(stderr.String(), "PLATFORMCTL_OUTPUT_WRITE") {
		t.Fatalf("missing diagnostic: %s", stderr.String())
	}
}
