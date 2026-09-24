package core

import "testing"

func TestRecordKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SSYXABC5184.RAW", "SSYXABC5184"},
		{"/data/a/SSYXABC5184.raw", "SSYXABC5184"},
		{`C:\tmp\SSYXABC5184.MP3`, "SSYXABC5184"},
		{"SSYXABC5184", "SSYXABC5184"},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := RecordKey(c.in); got != c.want {
			t.Fatalf("RecordKey(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
