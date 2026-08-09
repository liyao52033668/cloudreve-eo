package handler

import "testing"

func TestParseProxyRange(t *testing.T) {
	cases := []struct {
		header   string
		start    int64
		end      int64
		hasRange bool
	}{
		{"", 0, -1, false},
		{"bytes=0-499", 0, 499, true},
		{"bytes=500-", 500, -1, true},
		{"bytes=-500", -500, -1, true}, // 后缀范围，start 为负 suffix
		{"bytes=0-0", 0, 0, true},
		{"bytes=abc", 0, -1, false},
		{"bytes=5-2", 0, -1, false},
		{"bytes=0-99,200-299", 0, -1, false}, // 多范围不支持
	}
	for _, tc := range cases {
		s, e, ok := parseProxyRange(tc.header)
		if s != tc.start || e != tc.end || ok != tc.hasRange {
			t.Errorf("parseProxyRange(%q) = (%d,%d,%v), want (%d,%d,%v)",
				tc.header, s, e, ok, tc.start, tc.end, tc.hasRange)
		}
	}
}
