package models

import (
	"encoding/json"
	"testing"
)

func TestNoteIsStarPresence(t *testing.T) {
	var withStar, withoutStar Note
	if err := json.Unmarshal([]byte(`{"NoteId":"n","IsStar":true}`), &withStar); err != nil {
		t.Fatal(err)
	}
	if !withStar.IsStar || !withStar.IsStarPresent {
		t.Fatalf("expected present star: %+v", withStar)
	}
	if err := json.Unmarshal([]byte(`{"NoteId":"n"}`), &withoutStar); err != nil {
		t.Fatal(err)
	}
	if withoutStar.IsStarPresent {
		t.Fatal("missing IsStar reported as present")
	}
}
