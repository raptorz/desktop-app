package models

import "time"

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
