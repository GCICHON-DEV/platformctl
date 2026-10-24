package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualBinaryRedactsPlanProcessAndFailure(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "platformctl")
	if out, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "human", true: "json"}[machine], func(t *testing.T) {
			root := t.TempDir()
			const envSecret = "synthetic-env-canary-927"
			const argSecret = "synthetic-arg-canary-638"
			manifest := `apiVersion: platformctl.io/v1beta1
metadata: {name: synthetic, description: Synthetic secret redaction}
files:
  - path: generated/run.sh
    content: |
      printf '%s' "$PLATFORMCTL_SECRET_FIXTURE"
      printf '\n'
      printf '%s' "$2" >&2
      printf '\n' >&2
      exit 9
steps:
  apply:
    - {id: synthetic, name: Synthetic failure, command: sh, args: [generated/run.sh, --password, ` + argSecret + `]}
  destroy:
    - {name: noop, command: echo}
`
			writeCLIFile(t, filepath.Join(root, "manifest.yaml"), manifest)
			writeCLIFile(t, filepath.Join(root, "platform.yaml"), "template: {source: ./manifest.yaml}\n")
			for _, phase := range []string{"plan", "apply"} {
				args := []string{phase, "--verbose"}
				if machine {
					args = append(args, "--json")
				}
				if phase == "apply" {
					args = append(args, "--yes")
				}
				cmd := exec.Command(binary, args...)
				cmd.Dir = root
				cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "PLATFORMCTL_SECRET_FIXTURE=" + envSecret}
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				if phase == "plan" && err != nil {
					t.Fatalf("plan %v %s %s", err, stdout.String(), stderr.String())
				}
				if phase == "apply" && err == nil {
					t.Fatal("expected child failure")
				}
				for _, secret := range []string{envSecret, argSecret} {
					if strings.Contains(stdout.String()+stderr.String(), secret) {
						t.Fatalf("%s leaked %q: %s %s", phase, secret, stdout.String(), stderr.String())
					}
				}
				if machine {
					var doc interface{}
					if json.Unmarshal(stdout.Bytes(), &doc) != nil {
						t.Fatal("invalid JSON", stdout.String())
					}
				}
				if phase == "apply" && !strings.Contains(stderr.String(), "REDACTED") && !strings.Contains(stdout.String(), "REDACTED") {
					t.Fatal("missing child diagnostic marker")
				}
			}
			data, err := os.ReadFile(filepath.Join(root, ".platformctl/state.json"))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{envSecret, argSecret} {
				if bytes.Contains(data, []byte(secret)) {
					t.Fatal("state contains plaintext secret")
				}
			}
		})
	}
}
