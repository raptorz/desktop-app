package models

import "time"

type Notebook struct {
	ID               string     `json:"_id"`
	NotebookID       string     `json:"NotebookId"`
	ServerNotebookID string     `json:"ServerNotebookId,omitempty"`
	UserID           string     `json:"UserId"`
	ParentNotebookID string     `json:"ParentNotebookId,omitempty"`
	Title            string     `json:"Title"`
	Seq              int        `json:"Seq"`
	NumberNotes      int        `json:"NumberNotes"`
	UrlTitle         string     `json:"UrlTitle,omitempty"`
	IsBlog           bool       `json:"IsBlog"`
	IsTrash          bool       `json:"IsTrash"`
	Usn              int64      `json:"Usn"`
	IsDeleted        bool       `json:"IsDeleted,omitempty"`
	IsDirty          bool       `json:"IsDirty"`
	LocalIsNew       bool       `json:"LocalIsNew"`
	LocalIsDelete    bool       `json:"LocalIsDelete"`
	CreatedTime      *time.Time `json:"CreatedTime,omitempty"`
	UpdatedTime      *time.Time `json:"UpdatedTime,omitempty"`

	Subs []*Notebook `json:"Subs,omitempty"`
}

func NewNotebook() *Notebook {
	return &Notebook{
		Seq:        -1,
		IsDirty:    true,
		LocalIsNew: true,
	}
}
