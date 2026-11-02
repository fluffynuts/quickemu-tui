package testtool

import "testing"

func TestIsRequired(t *testing.T) {
	cases := []struct {
		tool, list string
		want       bool
	}{
		{"qemu-img", "", false},
		{"qemu-img", "qemu-img", true},
		{"qemu-img", "genisoimage, qemu-img", true},
		{"qemu-img", "genisoimage", false},
		{"qemu", "qemu-img", false},
	}
	for _, c := range cases {
		if got := isRequired(c.tool, c.list); got != c.want {
			t.Errorf("isRequired(%q, %q) = %v, want %v", c.tool, c.list, got, c.want)
		}
	}
}

func TestRequireReturnsPathOfInstalledTool(t *testing.T) {
	if got := Require(t, "go"); got == "" {
		t.Fatal("empty path for go")
	}
}
