package models

type SyncInfo struct {
	Notebook *SyncResult `json:"notebook"`
	Note     *SyncResult `json:"note"`
	Tag      *SyncResult `json:"tag"`
}

type SyncResult struct {
	Ok              bool            `json:"ok"`
	Adds            []string        `json:"adds"`
	Updates         []string        `json:"updates"`
	Deletes         []string        `json:"deletes"`
	Conflicts       []*SyncConflict `json:"conflicts"`
	ChangeAdds      []string        `json:"change_adds"`
	ChangeUpdates   []string        `json:"change_updates"`
	ChangeConflicts []string        `json:"change_conflicts"`
	ChangeNeedAdds  []string        `json:"change_need_adds"`
	Errors          []*SyncError    `json:"errors"`
	Msg             string          `json:"msg,omitempty"`
}

type SyncConflict struct {
	Server       *Note `json:"server"`
	Local        *Note `json:"local"`
	ConflictCopy *Note `json:"conflict_copy,omitempty"`
}

type SyncError struct {
	Err  string `json:"err"`
	Ret  string `json:"ret"`
	Note *Note  `json:"note"`
}

type ServerSyncState struct {
	Ok           bool   `json:"Ok"`
	LastSyncUsn  int64  `json:"LastSyncUsn"`
	LastSyncTime string `json:"LastSyncTime"`
	Msg          string `json:"Msg,omitempty"`
}

func NewSyncInfo() *SyncInfo {
	return &SyncInfo{
		Notebook: &SyncResult{},
		Note:     &SyncResult{},
		Tag:      &SyncResult{},
	}
}
