package server

import "testing"

func TestNormName(t *testing.T) {
	for in, want := range map[string]string{"  Алишер   Т. ": "Алишер Т.", "O'Brien-Smith": "O'Brien-Smith", "": ""} {
		if got, ok := normName(in); !ok || got != want {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"a", "<script>", "http://x.com", "Bob123", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, ok := normName(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}
