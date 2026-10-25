package toolchain

import (
	"reflect"
	"testing"

	"platformctl/internal/templateengine"
)

func TestDefaultVersionCheckContract(t *testing.T) {
	for _, tc := range []struct {
		tool templateengine.Tool
		want []string
	}{
		{templateengine.Tool{Name: "kubectl"}, []string{"version", "--client"}},
		{templateengine.Tool{Name: "terraform"}, []string{"version"}},
		{templateengine.Tool{Name: "sh"}, nil},
		{templateengine.Tool{Name: "custom", MinVersion: "1.0.0"}, []string{"--version"}},
		{templateengine.Tool{Name: "custom", VersionArgs: []string{"version"}}, []string{"version"}},
	} {
		if got := defaultVersionArgs(tc.tool); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%+v args=%v want=%v", tc.tool, got, tc.want)
		}
	}
}
