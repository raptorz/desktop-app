package api

import (
	"encoding/json"
	"testing"
)

func TestHistoryResponseContract(t *testing.T) {
	var metas HistoryListResponse
	if err := json.Unmarshal([]byte(`{"Ok":true,"Item":[{"Id":"version-1","Index":0,"UpdatedUserId":"user-1","UpdatedTime":"2025-01-02T03:04:05Z"}]}`), &metas); err != nil {
		t.Fatal(err)
	}
	if !metas.Ok || len(metas.Item) != 1 || metas.Item[0].ID != "version-1" || metas.Item[0].Index != 0 || metas.Item[0].UpdatedUserID != "user-1" {
		t.Fatalf("unexpected history metadata: %+v", metas)
	}

	var content HistoryContentResponse
	if err := json.Unmarshal([]byte(`{"Ok":true,"Item":{"HistoryId":"version-1","UpdatedUserId":"user-1","UpdatedTime":"2025-01-02T03:04:05Z","Content":"old content"}}`), &content); err != nil {
		t.Fatal(err)
	}
	if !content.Ok || content.Item == nil || content.Item.ID != "version-1" || content.Item.Content != "old content" {
		t.Fatalf("unexpected history content: %+v", content)
	}
}
