package models

import "time"

type Note struct {
	ID             string     `json:"_id"`
	NoteID         string     `json:"NoteId"`
	ServerNoteID   string     `json:"ServerNoteId,omitempty"`
	NotebookID     string     `json:"NotebookId"`
	UserID         string     `json:"UserId"`
	Title          string     `json:"Title,omitempty"`
	Content        string     `json:"Content,omitempty"`
	Desc           string     `json:"Desc,omitempty"`
	Abstract       string     `json:"Abstract,omitempty"`
	ImgSrc         string     `json:"ImgSrc,omitempty"`
	Tags           []string   `json:"Tags,omitempty"`
	IsMarkdown     bool       `json:"IsMarkdown"`
	IsTrash        bool       `json:"IsTrash"`
	IsBlog         bool       `json:"IsBlog"`
	IsStar         bool       `json:"IsStar"`
	IsDeleted      bool       `json:"IsDeleted,omitempty"`
	IsNew          bool       `json:"IsNew,omitempty"`
	Usn            int64      `json:"Usn"`
	IsDirty        bool       `json:"IsDirty"`
	ContentIsDirty bool       `json:"ContentIsDirty"`
	LocalIsNew     bool       `json:"LocalIsNew"`
	LocalIsDelete  bool       `json:"LocalIsDelete"`
	InitSync       bool       `json:"InitSync"`
	ConflictNoteID string     `json:"ConflictNoteId,omitempty"`
	ConflictTime   *time.Time `json:"ConflictTime,omitempty"`
	ConflictFixed  bool       `json:"ConflictFixed"`
	Err            string     `json:"Err,omitempty"`
	LocalContent   string     `json:"LocalContent,omitempty"`
	CreatedTime    *time.Time `json:"CreatedTime,omitempty"`
	UpdatedTime    *time.Time `json:"UpdatedTime,omitempty"`

	Attachs   []*Attach              `json:"Attachs,omitempty"`
	Files     []*FileRef             `json:"Files,omitempty"`
	FileDatas map[string]interface{} `json:"FileDatas,omitempty"`
}

type FileRef struct {
	FileID       string `json:"FileId"`
	LocalFileID  string `json:"LocalFileId,omitempty"`
	ServerFileID string `json:"ServerFileId,omitempty"`
	Type         string `json:"Type,omitempty"`
	HasBody      bool   `json:"HasBody"`
	IsAttach     bool   `json:"IsAttach"`
	Title        string `json:"Title,omitempty"`
	Path         string `json:"Path,omitempty"`
	IsDirty      bool   `json:"IsDirty"`
}

type NoteContentAPI struct {
	Ok      bool   `json:"Ok"`
	Content string `json:"Content"`
	NoteID  string `json:"NoteId"`
}

func NewNote() *Note {
	return &Note{
		IsDirty:    true,
		LocalIsNew: true,
	}
}
