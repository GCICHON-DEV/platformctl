package deps

import "testing"

func TestParseVersion(t *testing.T) {
	got, err := parseVersion("Terraform v1.5.7")
	if err != nil {
		t.Fatalf("parseVersion returned error: %v", err)
	}
	want := [3]int{1, 5, 7}
	if got != want {
		t.Fatalf("version = %#v, want %#v", got, want)
	}
}

func TestVersionAtLeast(t *testing.T) {
	ok, err := versionAtLeast("v1.34.0", "1.33.0")
	if err != nil {
		t.Fatalf("versionAtLeast returned error: %v", err)
	}
	if !ok {
		t.Fatalf("versionAtLeast returned false, want true")
	}
}

func TestVersionMatches(t *testing.T) {
	ok, err := versionMatches("v3.18.6+g123", "3.18.6")
	if err != nil {
		t.Fatalf("versionMatches returned error: %v", err)
	}
	if !ok {
		t.Fatalf("versionMatches returned false, want true")
	}
}

func TestVersionProblem(t *testing.T) {
	status := Status{
		Tool:      Tool{Name: "kubectl", Version: "v1.34.0"},
		Installed: true,
		Version:   "v1.33.0",
	}
	problem := VersionProblem(status)
	if problem == "" {
		t.Fatalf("VersionProblem returned empty string, want mismatch")
	}
}

func TestMissing(t *testing.T) {
	statuses := []Status{
		{Tool: Tool{Name: "helm"}, Installed: true},
		{Tool: Tool{Name: "kubectl"}, Installed: false},
	}
	missing := Missing(statuses)
	if len(missing) != 1 || missing[0].Name != "kubectl" {
		t.Fatalf("missing = %#v, want kubectl", missing)
	}
}
