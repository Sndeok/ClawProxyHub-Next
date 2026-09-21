package admin

import "testing"

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.5", "1.0.3", 1},
		{"v1.0.5", "1.0.5", 0},
		{"1.0.5-beta", "1.0.4", 1},
		{"1.0.0", "1", 0},
		{"1.0.10", "1.0.9", 1},
		{"1.0.9", "1.0.10", -1},
		{"1.0.3", "1.0.5", -1},
		{"1.0.5+build.7", "v1.0.5", 0},
		{"  V2.0.0  ", "1.99.99", 1},
		{"", "0", 0},
	}
	for _, tc := range cases {
		if got := compareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSplitSemver(t *testing.T) {
	cases := map[string][]int{
		"v1.0.5-beta": {1, 0, 5},
		"1.0.5+build": {1, 0, 5},
		"1.2":         {1, 2},
		"":            {0},
	}
	for input, want := range cases {
		got := splitSemver(input)
		if len(got) != len(want) {
			t.Errorf("splitSemver(%q) len=%d, want %d (%v)", input, len(got), len(want), want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("splitSemver(%q)[%d]=%d, want %d", input, i, got[i], want[i])
			}
		}
	}
}