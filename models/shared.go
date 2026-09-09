package models

import "time"

// SharedNotebook and SharedNote are kept separate from the personal sync
// models so cached shares can never enter the normal dirty/push pipeline.
type SharedNotebook struct {
	AccountID        string `json:"AccountId,omitempty"`
	NotebookID       string `json:"NotebookId"`
	OwnerUserID      string `json:"OwnerUserId"`
	ParentNotebookID string `json:"ParentNotebookId,omitempty"`
	Title            string `json:"Title"`
	Seq              int    `json:"Seq"`
	Perm             int    `json:"Perm"`
	Generation       int64  `json:"-"`
	Revoked          bool   `json:"-"`
}

type SharedNote struct {
	AccountID            string     `json:"AccountId,omitempty"`
	NoteID               string     `json:"NoteId"`
	NotebookID           string     `json:"NotebookId,omitempty"`
	OwnerUserID          string     `json:"OwnerUserId"`
	Title                string     `json:"Title"`
	Content              string     `json:"Content,omitempty"`
	Desc                 string     `json:"Desc,omitempty"`
	Tags                 []string   `json:"Tags,omitempty"`
	IsMarkdown           bool       `json:"IsMarkdown"`
	Perm                 int        `json:"Perm"`
	MetadataVersion      string     `json:"MetadataVersion"`
	TargetContentVersion string     `json:"Version"`
	CachedContentVersion string     `json:"-"`
	Digest               string     `json:"Digest,omitempty"`
	CacheState           string     `json:"CacheState,omitempty"`
	Generation           int64      `json:"-"`
	Revoked              bool       `json:"-"`
	CreatedTime          *time.Time `json:"CreatedTime,omitempty"`
	UpdatedTime          *time.Time `json:"UpdatedTime,omitempty"`
	CachedAt             *time.Time `json:"CachedAt,omitempty"`
}

type SharedFile struct {
	AccountID     string     `json:"AccountId,omitempty"`
	NoteID        string     `json:"NoteId"`
	FileID        string     `json:"FileId"`
	Kind          string     `json:"Kind"`
	Title         string     `json:"Title,omitempty"`
	Size          int64      `json:"Size,omitempty"`
	TargetVersion string     `json:"Version"`
	LocalPath     string     `json:"LocalPath,omitempty"`
	CacheState    string     `json:"CacheState,omitempty"`
	Retries       int        `json:"-"`
	NextRetryAt   int64      `json:"-"`
	Error         string     `json:"-"`
	Generation    int64      `json:"-"`
	Revoked       bool       `json:"-"`
	CachedAt      *time.Time `json:"CachedAt,omitempty"`
}

type SharedSnapshotItem struct {
	Kind     string              `json:"Kind"`
	Notebook *SharedNotebook     `json:"Notebook,omitempty"`
	Note     *SharedNote         `json:"Note,omitempty"`
	File     *SharedSnapshotFile `json:"File,omitempty"`
}

type SharedSnapshotFile struct {
	NoteID  string `json:"NoteId"`
	FileID  string `json:"FileId"`
	Kind    string `json:"Kind"`
	Title   string `json:"Title,omitempty"`
	Size    int64  `json:"Size"`
	Version string `json:"Version"`
}
