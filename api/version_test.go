package api

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.2.0", "1.1.9", 1},
		{"1.0.0", "1.1", -1},
		{"v2.0.0", "1.9.9", 1},
	} {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestServerVersionNotice(t *testing.T) {
	if got := ServerVersionNotice(&ServerVersion{Server: "pearlnote", Version: "1.0.0"}, nil); got != "" {
		t.Fatalf("compatible server notice = %q", got)
	}
	if got := ServerVersionNotice(&ServerVersion{Server: "pearlnote", Version: "1.0.0", MinVersion: "1.1.0"}, nil); got != "clientUpgradeRequired" {
		t.Fatalf("minimum version notice = %q", got)
	}
	if got := ServerVersionNotice(&ServerVersion{Server: "pearlnote", Version: "0.9.0"}, nil); got != "serverUpgradeRequired" {
		t.Fatalf("server version notice = %q", got)
	}
	if got := ServerVersionNotice(nil, &SharedAPIError{Status: 404}); got != "serverMigrationRequired" {
		t.Fatalf("legacy server notice = %q", got)
	}
}
