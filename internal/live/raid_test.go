package live

import "testing"

func TestRAIDController(t *testing.T) {
	for _, c := range []struct{ vendor, model, want string }{
		{"DELL    ", "PERC H740P Mini ", "PERC H740P Mini"},
		{"HP      ", "LOGICAL VOLUME  ", "Smart Array"},
		{"AVAGO   ", "MR9361-8i       ", "MR9361-8i"},
		{"Lenovo  ", "RAID 930-8i     ", "RAID 930-8i"},
		{"ATA     ", "ST8000NM0055-1RM", ""},
		{"QEMU    ", "QEMU HARDDISK   ", ""},
		{"", "Samsung SSD 990 PRO 2TB", ""},
	} {
		if got := raidController(c.vendor, c.model); got != c.want {
			t.Errorf("%q %q: got %q, want %q", c.vendor, c.model, got, c.want)
		}
	}
}
