package events

import (
	"fmt"
	"testing"
)

func TestTruncatedStringList(t *testing.T) {
	var saved int
	truncatedStringListMax, saved = 30, truncatedStringListMax
	defer func() { truncatedStringListMax = saved }()

	for _, tc := range []struct {
		desc  string
		count int
		want  string
	}{
		{"zero", 0, "[]"},
		{"one", 1, "[elt-0]"},
		{"not truncated", 4, "[elt-0, elt-1, elt-2, elt-3]"},
		{"truncated", 20, "[elt-0, elt-1, elt-2, elt-3, ...]"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var l []string
			for i := 0; i < tc.count; i++ {
				l = append(l, fmt.Sprintf("elt-%d", i))
			}
			got := TruncatedStringList(l)
			if got != tc.want {
				t.Errorf("TruncatedString(%v) = %q; want %q", l, got, tc.want)
			}
		})
	}
}
