package api

import (
	"encoding/json"
	"testing"
)

func TestLastSyncStateTimeAcceptsStringAndNumber(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"Ok":true,"LastSyncUsn":7,"LastSyncTime":"2026-09-13T00:00:00Z"}`, "2026-09-13T00:00:00Z"},
		{`{"Ok":true,"LastSyncUsn":7,"LastSyncTime":1694567890}`, "1694567890"},
	} {
		var state LastSyncStateResponse
		if err := json.Unmarshal([]byte(tc.body), &state); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.body, err)
		}
		if state.LastSyncTime != tc.want || state.LastSyncUsn != 7 || !state.Ok {
			t.Fatalf("state = %+v, want time %q", state, tc.want)
		}
	}
}
