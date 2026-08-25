package selfupdate

import "testing"

// A string compare puts 0.9.0 above 0.10.0, which is the version this silently
// gets wrong first.
func TestNewerComparesNumerically(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.10.0", "0.9.0", true},
		{"0.9.0", "0.10.0", false},
		{"0.1.2", "0.1.1", true},
		{"0.1.1", "0.1.1", false},
		{"1.0.0", "0.99.99", true},
		{"0.2", "0.1.9", true},
		{"junk", "0.1.0", false}, // degrades rather than crashing
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// The default from `go build` with no ldflags is "dev". Treating that as a
// version to compare told a machine three releases behind that it was current.
func TestLocalBuildsAreNeverCurrent(t *testing.T) {
	for _, v := range []string{"dev", "", "v0.2.0-dev", "0.1.0-rc1", "  dev  "} {
		if !IsLocalBuild(v) {
			t.Errorf("IsLocalBuild(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"0.1.1", "v0.1.1", "1.0.0"} {
		if IsLocalBuild(v) {
			t.Errorf("IsLocalBuild(%q) = true, want false", v)
		}
	}
}
