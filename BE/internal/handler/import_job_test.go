package handler

import "testing"

func TestParseItemPage(t *testing.T) {
	for _, tc := range []struct {
		limitRaw, offsetRaw   string
		wantLimit, wantOffset int
	}{
		{"", "", 200, 0},
		{"50", "10", 50, 10},
		{"0", "0", 1, 0},
		{"9999", "20", 500, 20},
		{"abc", "-5", 200, 0},
	} {
		limit, offset := parseItemPage(tc.limitRaw, tc.offsetRaw)
		if limit != tc.wantLimit || offset != tc.wantOffset {
			t.Errorf("parseItemPage(%q, %q) = %d, %d; want %d, %d",
				tc.limitRaw, tc.offsetRaw, limit, offset, tc.wantLimit, tc.wantOffset)
		}
	}
}
