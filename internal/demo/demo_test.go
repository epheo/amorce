package demo

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The demo and the VM test image stand in for the dotvirt appliance, so
// they refuse the names it refuses.
func TestReservedUsersAreTheAppliances(t *testing.T) {
	want := quoted(t, "../../appliances/dotvirt/Containerfile", `ARG RESERVED_USERS="([^"]*)"`)
	if got := quoted(t, "../../test/vm/names.Containerfile", `installer\.reserved-users="([^"]*)"`); got != want {
		t.Errorf("test/vm/names.Containerfile reserves %q, the appliance %q", got, want)
	}
	ps, err := (&System{}).Purposes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.ID == "virtualization" {
			if got := strings.Join(p.ReservedUsers, ";"); got != want {
				t.Errorf("the demo reserves %q, the appliance %q", got, want)
			}
			return
		}
	}
	t.Error("the demo has no virtualization purpose")
}

func quoted(t *testing.T, path, re string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(re).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s: no match for %s", path, re)
	}
	return string(m[1])
}
