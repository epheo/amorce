package media

import (
	"strings"
	"testing"
)

func TestNamesFromLabels(t *testing.T) {
	for _, tc := range []struct {
		labels map[string]string
		want   string // part of the error; empty means accepted
	}{
		{map[string]string{}, ""},
		{map[string]string{"domain": "console;git", "identity": "kanidm:idm", "settings": "/etc/dotvirt/appliance.env",
			"reserved-users": "admin;anonymous"}, ""},
		{map[string]string{"domain": "console", "settings": "/etc/dotvirt/appliance.env"}, ""},
		{map[string]string{"domain": "console"}, "under /etc"},
		{map[string]string{"domain": "console", "settings": "/var/lib/x.env"}, "under /etc"},
		{map[string]string{"domain": "console", "settings": "/etc/../var/x.env"}, "under /etc"},
		{map[string]string{"identity": "kanidm:idm", "settings": "/etc/x.env"}, "needs " + LabelPrefix + "domain"},
		{map[string]string{"domain": "console", "identity": "kanidm", "settings": "/etc/x.env"}, "provider:name"},
		{map[string]string{"domain": "con sole", "settings": "/etc/x.env"}, "one DNS label"},
		{map[string]string{"settings": "/etc/x.env"}, "nothing to write"},
	} {
		var im Image
		err := im.NamesFromLabels(func(k string) string { return tc.labels[k] })
		if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%v: %v, want %q", tc.labels, err, tc.want)
		}
	}
	var im Image
	im.NamesFromLabels(func(k string) string {
		return map[string]string{"domain": "console;git", "identity": "kanidm:idm", "settings": "/etc/a.env"}[k]
	})
	if len(im.Names) != 2 || im.Identity != "kanidm" || im.IdentityName != "idm" {
		t.Fatalf("%+v", im)
	}
}
