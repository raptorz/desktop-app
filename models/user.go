package models

import "time"

type User struct {
	ID            string     `json:"_id"`
	Username      string     `json:"username"`
	Email         string     `json:"email,omitempty"`
	Pwd           string     `json:"pwd,omitempty"`
	Token         string     `json:"token,omitempty"`
	Host          string     `json:"host,omitempty"`
	LastSyncUsn   int64      `json:"last_sync_usn"`
	LastSyncTime  *time.Time `json:"last_sync_time,omitempty"`
	NotebookUsn   int64      `json:"notebook_usn"`
	NoteUsn       int64      `json:"note_usn"`
	TagUsn        int64      `json:"tag_usn"`
	IsActive      bool       `json:"is_active"`
	IsLocal       bool       `json:"is_local"`
	HasDB         bool       `json:"has_db"`
	State         string     `json:"state,omitempty"`
	CreatedTime   *time.Time `json:"created_time,omitempty"`
	LastLoginTime *time.Time `json:"last_login_time,omitempty"`
}

type UserAPI struct {
	Ok       bool   `json:"Ok"`
	Msg      string `json:"Msg"`
	UserId   string `json:"UserId"`
	Username string `json:"Username"`
	Email    string `json:"Email"`
	Token    string `json:"Token"`
	Host     string `json:"Host"`
}

func (u *UserAPI) ToUser() *User {
	return &User{
		ID:       u.UserId,
		Username: u.Username,
		Email:    u.Email,
		Token:    u.Token,
		IsActive: true,
	}
}
