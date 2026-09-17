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

func TestCheckAPIResponseRejectsFailedServerResponse(t *testing.T) {
	if err := checkAPIResponse(&APIResponse{Ok: false, Msg: "conflict"}, "update note"); err == nil {
		t.Fatal("expected failed API response to be returned as an error")
	}
	if err := checkAPIResponse(&APIResponse{Ok: true}, "update note"); err != nil {
		t.Fatalf("unexpected error for successful API response: %v", err)
	}
}

func TestSyncListRejectsErrorEnvelope(t *testing.T) {
	if err := checkSyncListResponse(200, []byte(`{"Ok":false,"Msg":"NOTLOGIN"}`)); err == nil {
		t.Fatal("authentication error must not be treated as an empty sync page")
	}
	if err := checkSyncListResponse(200, []byte(`[]`)); err != nil {
		t.Fatalf("empty sync page should be valid: %v", err)
	}
}

func TestDecodeNoteResponseSupportsLeanoteDirectAndWrappedResponses(t *testing.T) {
	for _, body := range []string{
		`{"NoteId":"507f1f77bcf86cd799439011","Title":"direct","Usn":4}`,
		`{"Ok":true,"Note":{"NoteId":"507f1f77bcf86cd799439011","Title":"wrapped","Usn":5}}`,
	} {
		note, err := decodeNoteResponse([]byte(body), "update note")
		if err != nil || note == nil || note.NoteID == "" {
			t.Fatalf("decode %s: note=%+v err=%v", body, note, err)
		}
	}
	if _, err := decodeNoteResponse([]byte(`{"Ok":false,"Msg":"conflict"}`), "update note"); err == nil {
		t.Fatal("expected wrapped failure to be returned as an error")
	}
}

func TestDecodeNotebookResponseSupportsDirectResponse(t *testing.T) {
	notebook, err := decodeNotebookResponse([]byte(`{"NotebookId":"507f1f77bcf86cd799439011","Title":"direct"}`), "update notebook")
	if err != nil || notebook == nil || notebook.NotebookID == "" {
		t.Fatalf("decode notebook: notebook=%+v err=%v", notebook, err)
	}
}

func TestFlattenFormDataUsesLeanoteFieldNames(t *testing.T) {
	data := map[string]interface{}{
		"NoteId": "note-1",
		"Tags":   []string{"one", "two"},
		"Files":  []map[string]interface{}{{"LocalFileId": "file-1", "HasBody": true}},
	}
	form := flattenFormData(data)
	for key, want := range map[string]string{
		"NoteId":                "note-1",
		"Tags[0]":               "one",
		"Tags[1]":               "two",
		"Files[0][LocalFileId]": "file-1",
		"Files[0][HasBody]":     "true",
	} {
		if form[key] != want {
			t.Fatalf("form[%q] = %q, want %q (all=%v)", key, form[key], want, form)
		}
	}
}
