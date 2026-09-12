package models

import (
	"encoding/json"
	"time"
)

// HistoryMeta is the lightweight history record returned by the server.
// Content is fetched separately with GetHistoryContent.
type HistoryMeta struct {
	// ID is a stable identifier for this history version. It is preferred over
	// Index, which can change when a newer version is inserted.
	ID            string    `json:"Id,omitempty"`
	Index         int       `json:"Index"`
	UpdatedUserID string    `json:"UpdatedUserId"`
	UpdatedTime   time.Time `json:"UpdatedTime"`
}

// UnmarshalJSON accepts both the Gemsnote Id spelling and the more explicit
// HistoryId spelling so clients can interoperate during the API transition.
func (h *HistoryMeta) UnmarshalJSON(data []byte) error {
	type plain HistoryMeta
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.ID == "" {
		var aliases struct {
			HistoryID string `json:"HistoryId"`
		}
		if err := json.Unmarshal(data, &aliases); err != nil {
			return err
		}
		p.ID = aliases.HistoryID
	}
	*h = HistoryMeta(p)
	return nil
}

type HistoryEntry struct {
	ID            string    `json:"Id,omitempty"`
	UpdatedUserID string    `json:"UpdatedUserId"`
	UpdatedTime   time.Time `json:"UpdatedTime"`
	Content       string    `json:"Content"`
}

func (h *HistoryEntry) UnmarshalJSON(data []byte) error {
	type plain HistoryEntry
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.ID == "" {
		var aliases struct {
			HistoryID string `json:"HistoryId"`
		}
		if err := json.Unmarshal(data, &aliases); err != nil {
			return err
		}
		p.ID = aliases.HistoryID
	}
	*h = HistoryEntry(p)
	return nil
}

type Attach struct {
	ID           string     `json:"_id"`
	FileID       string     `json:"FileId"`
	ServerFileID string     `json:"ServerFileId,omitempty"`
	NoteID       string     `json:"NoteId"`
	UserID       string     `json:"UserId"`
	Title        string     `json:"Title,omitempty"`
	Type         string     `json:"Type,omitempty"`
	Path         string     `json:"Path,omitempty"`
	IsAttach     bool       `json:"IsAttach"`
	IsDirty      bool       `json:"IsDirty"`
	CreatedTime  *time.Time `json:"CreatedTime,omitempty"`
}

type Image struct {
	ID           string     `json:"_id"`
	FileID       string     `json:"FileId"`
	ServerFileID string     `json:"ServerFileId,omitempty"`
	UserID       string     `json:"UserId"`
	Path         string     `json:"Path,omitempty"`
	IsDirty      bool       `json:"IsDirty"`
	CreatedTime  *time.Time `json:"CreatedTime,omitempty"`
}

type NoteHistory struct {
	ID          int64      `json:"_id"`
	NoteID      string     `json:"NoteId"`
	Content     string     `json:"Content"`
	UpdatedTime *time.Time `json:"UpdatedTime"`
}
