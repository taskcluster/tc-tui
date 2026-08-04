package shell

import "testing"

func TestFormatRowCount(t *testing.T) {
	cases := []struct {
		name      string
		visible   int
		total     int
		truncated bool
		want      string
	}{
		{"exact", 42, 42, false, "42"},
		{"exact zero", 0, 0, false, "0"},
		{"truncated", 1000, 1000, true, "1000+"},
		{"filtered", 7, 213, false, "7 of 213"},
		{"filtered and truncated", 7, 213, true, "7 of 213+"},
		{"filtered to none", 0, 5, false, "0 of 5"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatRowCount(c.visible, c.total, c.truncated); got != c.want {
				t.Fatalf("formatRowCount(%d, %d, %v) = %q, want %q", c.visible, c.total, c.truncated, got, c.want)
			}
		})
	}
}
