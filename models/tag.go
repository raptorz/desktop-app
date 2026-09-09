package models

import "time"

type Tag struct {
	ID            string     `json:"_id"`
	Tag           string     `json:"Tag"`
	UserID        string     `json:"UserId"`
	Count         int        `json:"Count"`
	Usn           int64      `json:"Usn"`
	IsDirty       bool       `json:"IsDirty"`
	LocalIsDelete bool       `json:"LocalIsDelete"`
	CreatedTime   *time.Time `json:"CreatedTime,omitempty"`
	UpdatedTime   *time.Time `json:"UpdatedTime,omitempty"`
}
