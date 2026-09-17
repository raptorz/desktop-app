package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/gemsnote/gemsnote/api"
	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/service"
	"github.com/gemsnote/gemsnote/sharedsync"
	"github.com/gemsnote/gemsnote/sync"
	"github.com/gemsnote/gemsnote/utils"
)

type App struct {
	ctx         context.Context
	db          *db.Database
	api         *api.Client
	sync        *sync.SyncService
	sharedSync  *sharedsync.Service
	files       *service.FileService
	webCallback WebCallbackFunc
}

func NewApp(database *db.Database) *App {
	files := service.NewFileService(database)
	client := api.NewClient()
	return &App{
		db:   database,
		api:  client,
		sync: sync.NewSyncService(database, client),
		// Shared synchronization changes its API authentication context while it
		// runs, so it must not share a mutable client with personal sync.
		sharedSync: sharedsync.New(database, api.NewClient(), files),
		files:      files,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.sync = sync.NewSyncService(a.db, a.api)
	a.restoreSession()
	a.startAutoSync()
}

func (a *App) shutdown(ctx context.Context) {
	a.saveCurrentState()
	a.db.Close()
}

func (a *App) restoreSession() {
	user, err := a.db.GetActiveUser()
	if err != nil || user == nil {
		return
	}

	if user.Token != "" {
		a.api.SetToken(user.Token)
	}
	if user.Host != "" {
		a.api.SetHost(user.Host)
	}

	a.files.InitUserDirs(user.ID)
}

func (a *App) saveCurrentState() {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return
	}

	a.db.SetConfig("last_active_user", user.ID)
}

func (a *App) GetDataDir() string {
	return a.files.GetDataDir()
}

func (a *App) GetImageDir() string {
	return a.files.GetImageDir()
}

func (a *App) GetAttachDir() string {
	return a.files.GetAttachDir()
}

// ==================== User Operations ====================

func (a *App) Login(email, password, host string) map[string]interface{} {
	resp, err := a.api.Auth(email, password, host)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	if !resp.Ok {
		return map[string]interface{}{"Ok": false, "Msg": resp.Msg}
	}

	user := &models.User{
		ID:       resp.UserID,
		Username: resp.Username,
		Email:    resp.Email,
		Token:    resp.Token,
		Host:     host,
		IsActive: true,
	}

	a.db.InsertUser(user)
	a.db.SetCurrentUser(user.ID)
	a.api.SetToken(resp.Token)
	a.api.SetHost(host)
	a.files.InitUserDirs(user.ID)
	// A successful remote login always starts from a full server snapshot.
	// Keep the local cache for offline use, but never trust its old cursors.
	if err := a.sync.ForceFullSync(); err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	serverVersion, versionErr := a.api.GetServerVersion()
	result := map[string]interface{}{
		"Ok":       true,
		"UserId":   resp.UserID,
		"Username": resp.Username,
		"Email":    resp.Email,
		"Token":    resp.Token,
	}
	if serverVersion != nil {
		result["Server"] = serverVersion.Server
		result["ServerVersion"] = serverVersion.Version
		result["ServerMinVersion"] = serverVersion.MinVersion
	}
	if notice := api.ServerVersionNotice(serverVersion, versionErr); notice != "" {
		result["Notice"] = notice
	}

	return result
}

func (a *App) GetCurrentUser() map[string]interface{} {
	user, err := a.db.GetActiveUser()
	if err != nil || user == nil {
		return nil
	}

	return map[string]interface{}{
		"UserId":   user.ID,
		"Username": user.Username,
		"Email":    user.Email,
		"Token":    user.Token,
		"Host":     user.Host,
		"IsLocal":  user.IsLocal,
	}
}

func (a *App) Logout() map[string]interface{} {
	if user, _ := a.db.GetActiveUser(); user != nil && !user.IsLocal && user.Host != "" && user.Token != "" {
		if _, err := a.sync.FullSync(); err != nil {
			return map[string]interface{}{"Ok": false, "Msg": "syncFailed"}
		}
	}
	a.api.Logout()
	a.api.SetToken("")
	a.api.SetHost("")
	user, _ := a.db.GetActiveUser()
	if user != nil {
		a.db.UpdateUserToken(user.ID, "")
	}
	a.db.DeactivateAllUsers()
	a.db.SetCurrentUser("")
	return map[string]interface{}{"Ok": true}
}

func (a *App) CreateLocalAccount(username string) map[string]interface{} {
	result, err := a.files.CreateLocalAccount(username)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

func (a *App) GetAllUsers() []map[string]interface{} {
	users, err := a.files.GetAllUsers()
	if err != nil {
		return nil
	}
	return users
}

func (a *App) SwitchUser(userID string) {
	a.files.SwitchUser(userID)
	user, _ := a.db.GetUser(userID)
	if user != nil {
		if user.Token != "" {
			a.api.SetToken(user.Token)
		}
		if user.Host != "" {
			a.api.SetHost(user.Host)
		}
		a.files.InitUserDirs(userID)
	}
}

func (a *App) DeleteUser(userID string) {
	a.files.DeleteUser(userID)
}

func (a *App) IsLocal() bool {
	user, _ := a.db.GetActiveUser()
	return user != nil && user.IsLocal
}

func (a *App) GetToken() string {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return ""
	}
	return user.Token
}

func (a *App) GetHost() string {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return ""
	}
	return user.Host
}

func (a *App) GetDataStats() map[string]int64 {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}
	return a.files.GetDataStats(user.ID)
}

func (a *App) GetLastSyncState() map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}
	lastUsn, notebookUsn, noteUsn, tagUsn, err := a.db.GetAllLastSyncState(user.ID)
	if err != nil {
		return nil
	}
	return map[string]interface{}{
		"LastSyncUsn": lastUsn,
		"NotebookUsn": notebookUsn,
		"NoteUsn":     noteUsn,
		"TagUsn":      tagUsn,
	}
}

func (a *App) FullSyncForce() map[string]interface{} {
	err := a.sync.ForceFullSync()
	result := map[string]interface{}{"Ok": true}
	if err != nil {
		result = map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	// Mark the event so the SPA can distinguish a full-sync success (which
	// gets a green confirmation) from an incremental sync (which only clears
	// its pending marker). Keep the Wails method return payload unchanged.
	eventResult := map[string]interface{}{}
	for key, value := range result {
		eventResult[key] = value
	}
	eventResult["Full"] = true
	a.emitSyncFinished(eventResult)
	return result
}

// emitSyncFinished tells the SPA a sync round just ended so it can reload the
// lists that were rendered from the local database before the sync completed.
func (a *App) emitSyncFinished(result map[string]interface{}) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "sync-finished", result)
}

// ==================== Notebook Operations ====================

func (a *App) GetNotebooks() []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}

	notebooks, err := a.db.GetNotebooks(user.ID)
	if err != nil {
		return nil
	}

	mapped := a.db.MapNotebooks(notebooks)
	return notebooksToMaps(mapped)
}

func (a *App) AddNotebook(title, parentID string) map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}

	notebook := &models.Notebook{
		ID:               utils.ObjectId(),
		NotebookID:       utils.ObjectId(),
		Title:            title,
		UserID:           user.ID,
		ParentNotebookID: parentID,
		IsDirty:          true,
		LocalIsNew:       true,
	}

	a.db.InsertNotebook(notebook)

	return map[string]interface{}{
		"NotebookId": notebook.NotebookID,
		"Title":      notebook.Title,
	}
}

func (a *App) UpdateNotebookTitle(notebookID, title string) {
	a.db.UpdateNotebook(&models.Notebook{
		NotebookID: notebookID,
		Title:      title,
		IsDirty:    true,
	})
}

func (a *App) DeleteNotebook(notebookID string) bool {
	hasNotes, _ := a.db.HasNotes(notebookID)
	if hasNotes {
		return false
	}
	a.db.DeleteNotebook(notebookID)
	return true
}

func (a *App) DragNotebooks(notebookID, parentID string, seq int) {
	nb, err := a.db.GetNotebook(notebookID)
	if err != nil || nb == nil {
		return
	}
	nb.ParentNotebookID = parentID
	nb.Seq = seq
	nb.IsDirty = true
	a.db.UpdateNotebook(nb)
}

// ==================== Note Operations ====================

func (a *App) GetNotes(notebookID string) []map[string]interface{} {
	notes, err := a.db.GetNotes(notebookID)
	if err != nil {
		return nil
	}
	return notesToMaps(notes)
}

func (a *App) GetTrashNotes() []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}
	notes, err := a.db.GetTrashNotes(user.ID)
	if err != nil {
		return nil
	}
	return notesToMaps(notes)
}

func (a *App) GetStarNotes() []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}
	notes, err := a.db.GetStarNotes(user.ID)
	if err != nil {
		return nil
	}
	return notesToMaps(notes)
}

func (a *App) GetNote(noteID string) map[string]interface{} {
	note, err := a.db.GetNote(noteID)
	if err != nil || note == nil {
		return nil
	}
	return noteToMap(note)
}

func (a *App) GetNoteContent(noteID string) map[string]interface{} {
	note, err := a.db.GetNote(noteID)
	if err != nil || note == nil {
		return nil
	}

	if note.InitSync && note.ServerNoteID != "" {
		content, err := a.api.GetNoteContent(note.ServerNoteID)
		if err != nil {
			return nil
		}

		user, _ := a.db.GetActiveUser()
		if user != nil && user.Host != "" {
			content = utils.FixNoteContent(content, user.Host, "leanote://file/getImage")
		}

		a.db.UpdateNoteContent(noteID, content)
		note.Content = content
	}

	return map[string]interface{}{
		"NoteId":  note.NoteID,
		"Content": note.Content,
	}
}

func (a *App) UpdateNote(noteID, title, content string, tags []string) {
	note, err := a.db.GetNote(noteID)
	if err != nil || note == nil {
		return
	}

	if title != "" {
		note.Title = title
	}
	if content != "" {
		a.files.AddNoteHistory(noteID, note.Content)
		note.Content = content
		note.ContentIsDirty = true
	}
	if tags != nil {
		note.Tags = tags
	}
	note.IsDirty = true

	a.db.UpdateNote(note)
}

func (a *App) DeleteNote(noteID string) {
	a.files.DeleteNoteFiles(noteID)
	a.db.DeleteNote(noteID)

	note, _ := a.db.GetNote(noteID)
	if note != nil {
		a.db.CountNotes(note.NotebookID)
	}
}

func (a *App) MoveNote(noteID, notebookID string) {
	note, _ := a.db.GetNote(noteID)
	if note == nil {
		return
	}
	oldNotebookID := note.NotebookID
	a.db.MoveNote(noteID, notebookID)
	a.db.CountNotes(oldNotebookID)
	a.db.CountNotes(notebookID)
}

func (a *App) StarNote(noteID string) {
	a.db.StarNote(noteID)
}

func (a *App) SetNoteBlog(noteID string, isBlog bool) {
	note, _ := a.db.GetNote(noteID)
	if note == nil {
		return
	}
	note.IsBlog = isBlog
	note.IsDirty = true
	a.db.UpdateNote(note)
}

func (a *App) SearchNotes(keyword string) []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}
	notes, err := a.db.SearchNotes(user.ID, keyword)
	if err != nil {
		return nil
	}
	return notesToMaps(notes)
}

func (a *App) SearchNotesByTag(tag string) []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}
	notes, err := a.db.SearchNotesByTag(user.ID, tag)
	if err != nil {
		return nil
	}
	return notesToMaps(notes)
}

func (a *App) CopyNote(noteID, targetNotebookID string) map[string]interface{} {
	result, err := a.files.CopyNote(noteID, targetNotebookID)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

func (a *App) ClearTrash() {
	a.files.ClearTrash()
}

// ==================== Tag Operations ====================

func (a *App) GetTags() []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}

	tags, err := a.db.GetTags(user.ID)
	if err != nil {
		return nil
	}

	result := make([]map[string]interface{}, len(tags))
	for i, tag := range tags {
		result[i] = map[string]interface{}{
			"Tag":   tag.Tag,
			"Count": tag.Count,
		}
	}
	return result
}

func (a *App) AddTag(tagName string) {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return
	}
	a.db.AddOrUpdateTag(user.ID, tagName, false, 0)
}

func (a *App) DeleteTag(tagName string) {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return
	}
	a.db.DeleteTag(user.ID, tagName)
}

// ==================== File Operations ====================

func (a *App) PasteImage(base64Data string) map[string]interface{} {
	result, err := a.files.PasteImage(base64Data)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

func (a *App) CopyFile(srcPath string, isImage bool) map[string]interface{} {
	result, err := a.files.CopyFile(srcPath, isImage)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

func (a *App) AddAttach(srcPath, noteID string) map[string]interface{} {
	result, err := a.files.AddAttach(srcPath, noteID)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

func (a *App) GetImage(fileID string) map[string]interface{} {
	path, err := a.files.GetImage(fileID)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	ext := filepath.Ext(path)
	if len(ext) > 0 {
		ext = ext[1:]
	}

	return map[string]interface{}{
		"Ok":   true,
		"Data": base64.StdEncoding.EncodeToString(data),
		"Type": ext,
		"Path": path,
	}
}

func (a *App) GetAttach(fileID string) map[string]interface{} {
	path, title, err := a.files.GetAttach(fileID)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	return map[string]interface{}{
		"Ok":    true,
		"Path":  path,
		"Title": title,
	}
}

func (a *App) GetAttachsByNote(noteID string) []map[string]interface{} {
	attachs, err := a.files.GetAttachsByNote(noteID)
	if err != nil {
		return nil
	}

	result := make([]map[string]interface{}, len(attachs))
	for i, att := range attachs {
		result[i] = map[string]interface{}{
			"FileId":   att.FileID,
			"Title":    att.Title,
			"Type":     att.Type,
			"Path":     att.Path,
			"IsAttach": att.IsAttach,
		}
	}
	return result
}

func (a *App) DeleteAttach(fileID string) {
	a.files.DeleteAttach(fileID)
}

func (a *App) DownloadImage(url string) map[string]interface{} {
	result, err := a.files.DownloadImage(url)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

func (a *App) GetFileBase64(path string) string {
	result, err := a.files.GetFileBase64(path)
	if err != nil {
		return ""
	}
	return result
}

func (a *App) GetFileMD5(path string) string {
	result, err := a.files.GetFileMD5(path)
	if err != nil {
		return ""
	}
	return result
}

func (a *App) SaveFile(path, content string) bool {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)
	err := os.WriteFile(path, []byte(content), 0644)
	return err == nil
}

func (a *App) ReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// ==================== Export ====================

func (a *App) ExportNote(noteID, format string) map[string]interface{} {
	path, err := a.files.ExportNoteContent(noteID, format)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return map[string]interface{}{
		"Ok":   true,
		"Path": path,
	}
}

func (a *App) ExportPDFServer(noteID string) map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil || user.Host == "" {
		return map[string]interface{}{"Ok": false, "Msg": "no active user or host"}
	}

	note, err := a.db.GetNote(noteID)
	if err != nil || note == nil {
		return map[string]interface{}{"Ok": false, "Msg": "note not found"}
	}

	if note.ServerNoteID != "" {
		pdfPath, filename, err := a.api.GetAttach(note.ServerNoteID, a.files.GetAttachDir())
		if err != nil {
			return map[string]interface{}{"Ok": false, "Msg": err.Error()}
		}
		return map[string]interface{}{
			"Ok":       true,
			"Path":     pdfPath,
			"Filename": filename,
		}
	}

	return map[string]interface{}{"Ok": false, "Msg": "note not synced"}
}

func (a *App) ExportPDFLocal(noteID string) map[string]interface{} {
	note, err := a.db.GetNote(noteID)
	if err != nil || note == nil {
		return map[string]interface{}{"Ok": false, "Msg": "note not found"}
	}

	user, _ := a.db.GetActiveUser()
	exportDir := a.files.GetDataDir()
	if user != nil {
		exportDir = filepath.Join(a.files.GetUserDir(user.ID), "export")
	}
	os.MkdirAll(exportDir, 0755)

	htmlPath := filepath.Join(exportDir, noteID+".html")
	content := note.Content

	if note.IsMarkdown {
		content = "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>" +
			note.Title + "</title></head><body><pre>" + content + "</pre></body></html>"
	} else {
		content = "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>" +
			note.Title + "</title></head><body>" + content + "</body></html>"
	}

	if err := os.WriteFile(htmlPath, []byte(content), 0644); err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	return map[string]interface{}{
		"Ok":       true,
		"HtmlPath": htmlPath,
	}
}

// ==================== Note History ====================

func (a *App) AddNoteHistory(noteID, content string) {
	a.files.AddNoteHistory(noteID, content)
}

func (a *App) GetNoteHistories(noteID string) []map[string]interface{} {
	histories, err := a.files.GetNoteHistories(noteID)
	if err != nil {
		return nil
	}
	return histories
}

func (a *App) DeleteNoteHistory(noteID string) {
	a.db.DeleteNoteHistories(noteID)
}

// ==================== Sync Operations ====================

func (a *App) FullSync() map[string]interface{} {
	info, err := a.sync.FullSync()
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	return map[string]interface{}{
		"Ok": true,
		"Notebook": map[string]interface{}{
			"Adds":    info.Notebook.Adds,
			"Deletes": info.Notebook.Deletes,
		},
		"Note": map[string]interface{}{
			"Adds":      info.Note.Adds,
			"Updates":   info.Note.Updates,
			"Deletes":   info.Note.Deletes,
			"Conflicts": info.Note.Conflicts,
		},
		"Tag": map[string]interface{}{
			"Adds": info.Tag.Adds,
		},
	}
}

func (a *App) IncrSync() map[string]interface{} {
	info, err := a.sync.IncrSync()
	if err != nil {
		result := map[string]interface{}{"Ok": false, "Msg": err.Error()}
		a.emitSyncFinished(result)
		return result
	}

	result := map[string]interface{}{
		"Ok": true,
		"Notebook": map[string]interface{}{
			"Adds":          info.Notebook.Adds,
			"ChangeAdds":    info.Notebook.ChangeAdds,
			"ChangeUpdates": info.Notebook.ChangeUpdates,
		},
		"Note": map[string]interface{}{
			"Adds":          info.Note.Adds,
			"Updates":       info.Note.Updates,
			"ChangeAdds":    info.Note.ChangeAdds,
			"ChangeUpdates": info.Note.ChangeUpdates,
			"Conflicts":     info.Note.Conflicts,
			"Errors":        info.Note.Errors,
		},
		"Tag": map[string]interface{}{
			"Adds": info.Tag.Adds,
		},
	}
	a.emitSyncFinished(result)
	return result
}

func (a *App) IsSyncing() bool {
	return a.sync.IsSyncing()
}

// ==================== Config Operations ====================

func (a *App) GetConfig(key string) string {
	val, _ := a.files.GetConfig(key)
	return val
}

func (a *App) SetConfig(key, value string) {
	a.files.SetConfig(key, value)
}

func (a *App) GetUserConfig(key string) map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}

	configKey := fmt.Sprintf("user_%s_%s", user.ID, key)
	val, err := a.files.GetConfig(configKey)
	if err != nil || val == "" {
		return nil
	}

	return map[string]interface{}{
		"Key":   key,
		"Value": val,
	}
}

func (a *App) SetUserConfig(key, value string) {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return
	}

	configKey := fmt.Sprintf("user_%s_%s", user.ID, key)
	a.files.SetConfig(configKey, value)
}

func (a *App) GetGlobalConfig() map[string]interface{} {
	theme, _ := a.files.GetConfig("theme")
	leftWidth, _ := a.files.GetConfig("left_width")
	noteListWidth, _ := a.files.GetConfig("note_list_width")
	noteWidth, _ := a.files.GetConfig("note_width")
	version, _ := a.files.GetConfig("version")

	return map[string]interface{}{
		"Theme":         theme,
		"LeftWidth":     leftWidth,
		"NoteListWidth": noteListWidth,
		"NoteWidth":     noteWidth,
		"Version":       version,
	}
}

func (a *App) SetGlobalConfig(config map[string]string) {
	for k, v := range config {
		a.files.SetConfig(k, v)
	}
}

func (a *App) SaveCurState(state map[string]string) {
	for k, v := range state {
		a.files.SetConfig("state_"+k, v)
	}
}

func (a *App) GetCurState() map[string]string {
	keys := []string{"left_width", "note_list_width", "note_width", "last_note_id", "last_notebook_id"}
	state := make(map[string]string)
	for _, k := range keys {
		val, _ := a.files.GetConfig("state_" + k)
		if val != "" {
			state[k] = val
		}
	}
	return state
}

// ==================== Helper Functions ====================

func notebooksToMaps(notebooks []*models.Notebook) []map[string]interface{} {
	result := make([]map[string]interface{}, len(notebooks))
	for i, nb := range notebooks {
		m := map[string]interface{}{
			"NotebookId":       nb.NotebookID,
			"Title":            nb.Title,
			"Seq":              nb.Seq,
			"ParentNotebookId": nb.ParentNotebookID,
			"NumberNotes":      nb.NumberNotes,
			"IsBlog":           nb.IsBlog,
		}
		if len(nb.Subs) > 0 {
			m["Subs"] = notebooksToMaps(nb.Subs)
		}
		result[i] = m
	}
	return result
}

func notesToMaps(notes []*models.Note) []map[string]interface{} {
	result := make([]map[string]interface{}, len(notes))
	for i, note := range notes {
		result[i] = noteToMap(note)
	}
	return result
}

func noteToMap(note *models.Note) map[string]interface{} {
	return map[string]interface{}{
		"NoteId":       note.NoteID,
		"NotebookId":   note.NotebookID,
		"Title":        note.Title,
		"Desc":         note.Desc,
		"ImgSrc":       note.ImgSrc,
		"Tags":         note.Tags,
		"IsMarkdown":   note.IsMarkdown,
		"IsBlog":       note.IsBlog,
		"IsTrash":      note.IsTrash,
		"IsStar":       note.IsStar,
		"CreatedTime":  note.CreatedTime,
		"UpdatedTime":  note.UpdatedTime,
		"IsDirty":      note.IsDirty,
		"LocalIsNew":   note.LocalIsNew,
		"ServerNoteId": note.ServerNoteID,
	}
}

// ==================== Local User Login with Password ====================

func (a *App) LoginLocal(username, password string) map[string]interface{} {
	user, err := a.db.GetUserByUsername(username)
	if err != nil || user == nil {
		return map[string]interface{}{"Ok": false, "Msg": "user not found"}
	}

	if !user.IsLocal {
		return map[string]interface{}{"Ok": false, "Msg": "not a local account"}
	}

	if user.Pwd == "" {
		return map[string]interface{}{"Ok": false, "Msg": "password not set"}
	}

	hashedPwd := utils.MD5WithSalt(password, user.ID)

	if len(user.Pwd) == 32 {
		if user.Pwd != hashedPwd {
			return map[string]interface{}{"Ok": false, "Msg": "password incorrect"}
		}
	} else {
		if user.Pwd != password {
			return map[string]interface{}{"Ok": false, "Msg": "password incorrect"}
		}
		a.db.UpdateUserPwd(user.ID, hashedPwd)
	}

	a.db.SetCurrentUser(user.ID)
	a.files.InitUserDirs(user.ID)
	a.db.UpdateLastLoginTime(user.ID)

	return map[string]interface{}{
		"Ok":       true,
		"UserId":   user.ID,
		"Username": user.Username,
		"IsLocal":  true,
	}
}

func (a *App) CreateLocalAccountWithPwd(username, password string) map[string]interface{} {
	userID := utils.ObjectId()
	now := time.Now()
	pwd := utils.MD5WithSalt(password, userID)

	user := &models.User{
		ID:          userID,
		Username:    username,
		Pwd:         pwd,
		IsActive:    true,
		IsLocal:     true,
		CreatedTime: &now,
	}

	if err := a.db.InsertUser(user); err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	a.db.SetCurrentUser(userID)
	a.files.InitUserDirs(userID)

	defaultNbID := utils.ObjectId()
	defaultNb := &models.Notebook{
		ID:         utils.ObjectId(),
		NotebookID: defaultNbID,
		Title:      "Gemsnote",
		UserID:     userID,
		Seq:        0,
	}
	a.db.InsertNotebook(defaultNb)

	defaultNoteID := utils.ObjectId()
	defaultNote := &models.Note{
		ID:          utils.ObjectId(),
		NoteID:      defaultNoteID,
		NotebookID:  defaultNbID,
		UserID:      userID,
		Title:       "Welcome to Gemsnote",
		Content:     "<h2>Gemsnote 珠玑笔记</h2><p>Welcome!</p>",
		Desc:        "Gemsnote 珠玑笔记",
		Tags:        []string{"Gemsnote", "Welcome"},
		IsDirty:     true,
		LocalIsNew:  true,
		CreatedTime: &now,
		UpdatedTime: &now,
	}
	a.db.InsertNote(defaultNote)
	a.db.CountNotes(defaultNbID)

	a.db.AddOrUpdateTag(userID, "Gemsnote", false, 0)
	a.db.AddOrUpdateTag(userID, "Welcome", false, 0)

	return map[string]interface{}{
		"Ok":              true,
		"UserId":          userID,
		"Username":        username,
		"IsLocal":         true,
		"DefaultNotebook": defaultNbID,
		"DefaultNote":     defaultNoteID,
	}
}

// ==================== Batch Note Operations ====================

func (a *App) DeleteNotes(noteIDs []string) map[string]interface{} {
	results := make([]map[string]interface{}, len(noteIDs))
	for i, noteID := range noteIDs {
		a.DeleteNote(noteID)
		results[i] = map[string]interface{}{"NoteId": noteID, "Ok": true}
	}
	return map[string]interface{}{"Ok": true, "Results": results}
}

func (a *App) MoveNotes(noteIDs []string, notebookID string) map[string]interface{} {
	results := make([]map[string]interface{}, len(noteIDs))
	for i, noteID := range noteIDs {
		a.MoveNote(noteID, notebookID)
		results[i] = map[string]interface{}{"NoteId": noteID, "Ok": true}
	}
	return map[string]interface{}{"Ok": true, "Results": results}
}

func (a *App) CopyNotes(noteIDs []string, notebookID string) map[string]interface{} {
	results := make([]map[string]interface{}, len(noteIDs))
	for i, noteID := range noteIDs {
		result := a.CopyNote(noteID, notebookID)
		results[i] = result
	}
	return map[string]interface{}{"Ok": true, "Results": results}
}

func (a *App) SetNotesBlog(noteIDs []string, isBlog bool) map[string]interface{} {
	for _, noteID := range noteIDs {
		a.SetNoteBlog(noteID, isBlog)
	}
	return map[string]interface{}{"Ok": true}
}

func (a *App) ConflictIsFixed(noteID string) {
	note, _ := a.db.GetNote(noteID)
	if note != nil {
		note.ConflictNoteID = ""
		note.ConflictFixed = true
		a.db.UpdateNote(note)
	}
}

// ==================== Sync Control ====================

func (a *App) StopSync() {
	a.sync.Stop()
}

func (a *App) SetSyncProgressCallback() {
	a.sync.SetProgressCallback(func(stage string, current, total int) {
		if a.webCallback != nil {
			a.webCallback("syncProgress", map[string]interface{}{
				"Stage":   stage,
				"Current": current,
				"Total":   total,
			})
		}
	})
}

// ==================== Image Upload for Editor ====================

func (a *App) UploadImage(imagePath string) map[string]interface{} {
	if imagePath == "" {
		return map[string]interface{}{"Ok": false, "Msg": "path is empty"}
	}

	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		return map[string]interface{}{"Ok": false, "Msg": "file not exists"}
	}

	ext := filepath.Ext(imagePath)
	if len(ext) > 0 {
		ext = ext[1:]
	}

	if !utils.IsImageExt(ext) {
		return map[string]interface{}{"Ok": false, "Msg": "not an image file"}
	}

	return a.CopyFile(imagePath, true)
}

// ==================== MIME Type ====================

func (a *App) GetMIMEType(ext string) string {
	return utils.GetMIMEType(ext)
}

// ==================== Web Callback Bridge ====================

type WebCallbackFunc func(event string, data interface{})

var webCallback WebCallbackFunc

func (a *App) SetWebCallback(cb WebCallbackFunc) {
	webCallback = cb
}

func (a *App) EmitWebEvent(event string, data interface{}) {
	if webCallback != nil {
		webCallback(event, data)
	}
}

func (a *App) SyncFinished(hasError bool) {
	a.EmitWebEvent("syncFinished", map[string]interface{}{"HasError": hasError})
}

func (a *App) ContentSynced(noteID, content string) {
	a.EmitWebEvent("contentSynced", map[string]interface{}{
		"NoteId":  noteID,
		"Content": content,
	})
}

func (a *App) AttachSynced(fileID, noteID string) {
	a.EmitWebEvent("attachSynced", map[string]interface{}{
		"FileId": fileID,
		"NoteId": noteID,
	})
}

func (a *App) SyncProcess(entityType, title string) {
	a.EmitWebEvent("syncProcess", map[string]interface{}{
		"Type":  entityType,
		"Title": title,
	})
}

func (a *App) NotebooksReloaded() {
	a.EmitWebEvent("reloadNotebook", nil)
}

func (a *App) NoteAdded(noteID string) {
	note, _ := a.db.GetNote(noteID)
	if note != nil {
		a.EmitWebEvent("addSyncNote", noteToMap(note))
	}
}

func (a *App) NoteUpdated(noteID string) {
	note, _ := a.db.GetNote(noteID)
	if note != nil {
		a.EmitWebEvent("updateSyncNote", noteToMap(note))
	}
}

func (a *App) NoteDeleted(noteID string) {
	a.EmitWebEvent("deleteSyncNote", map[string]interface{}{"NoteId": noteID})
}

func (a *App) ConflictNote(conflict *models.SyncConflict) {
	a.EmitWebEvent("fixSyncConflictNote", map[string]interface{}{
		"Server":       noteToMap(conflict.Server),
		"Local":        noteToMap(conflict.Local),
		"ConflictCopy": noteToMap(conflict.ConflictCopy),
	})
}

func (a *App) SyncError(err *models.SyncError) {
	a.EmitWebEvent("syncError", map[string]interface{}{
		"NoteId": err.Note.NoteID,
		"Error":  err.Err,
	})
}

// ==================== Additional User Operations ====================

func (a *App) GetUserDBPath(userID string) string {
	return a.files.GetUserDBPath(userID)
}

func (a *App) GetUserImagesPath(userID string) string {
	return a.files.GetUserImagesPath(userID)
}

func (a *App) GetUserAttachsPath(userID string) string {
	return a.files.GetUserAttachsPath(userID)
}

func (a *App) GetUser(userID string) map[string]interface{} {
	user, err := a.db.GetUser(userID)
	if err != nil || user == nil {
		return nil
	}
	return map[string]interface{}{
		"UserId":        user.ID,
		"Username":      user.Username,
		"Email":         user.Email,
		"Host":          user.Host,
		"IsActive":      user.IsActive,
		"IsLocal":       user.IsLocal,
		"LastSyncUsn":   user.LastSyncUsn,
		"LastSyncTime":  user.LastSyncTime,
		"CreatedTime":   user.CreatedTime,
		"LastLoginTime": user.LastLoginTime,
	}
}

func (a *App) GetUserDBDataStats(userID string) map[string]interface{} {
	notebookCount, _ := a.db.CountNotebooks(userID)
	noteCount, _ := a.db.CountAllNotes(userID)
	tagCount, _ := a.db.CountTags(userID)
	return map[string]interface{}{
		"NotebookCount": notebookCount,
		"NoteCount":     noteCount,
		"TagCount":      tagCount,
	}
}

// ==================== Additional DB Methods ====================

func (a *App) GetNoteByServerNoteId(serverNoteID string) map[string]interface{} {
	note, err := a.db.GetNoteByServerID(serverNoteID)
	if err != nil || note == nil {
		return nil
	}
	return noteToMap(note)
}

func (a *App) GetLocalNoteId(serverNoteID string) string {
	noteID, _ := a.db.GetLocalNoteID(serverNoteID)
	return noteID
}

func (a *App) GetServerNoteId(noteID string) string {
	serverID, _ := a.db.GetServerNoteID(noteID)
	return serverID
}

func (a *App) SetNoteNotDirty(noteID string) {
	a.db.SetNoteNotDirty(noteID)
}

func (a *App) SetNoteNotDirtyNotDelete(noteID string) {
	note, _ := a.db.GetNote(noteID)
	if note != nil {
		note.IsDirty = false
		note.LocalIsDelete = false
		a.db.UpdateNote(note)
	}
}

func (a *App) SetNoteIsNew(noteID string) {
	note, _ := a.db.GetNote(noteID)
	if note != nil {
		note.LocalIsNew = true
		note.IsDirty = true
		a.db.UpdateNote(note)
	}
}

func (a *App) SetNoteError(noteID, errMsg string) {
	a.db.SetNoteError(noteID, errMsg)
}

func (a *App) GetNotebookByServerId(serverNotebookID string) map[string]interface{} {
	nb, err := a.db.GetNotebookByServerID(serverNotebookID)
	if err != nil || nb == nil {
		return nil
	}
	return map[string]interface{}{
		"NotebookId":       nb.NotebookID,
		"Title":            nb.Title,
		"ParentNotebookId": nb.ParentNotebookID,
		"Seq":              nb.Seq,
	}
}

func (a *App) GetLocalNotebookId(serverNotebookID string) string {
	nbID, _ := a.db.GetNotebookIDByServerID(serverNotebookID)
	return nbID
}

func (a *App) GetServerNotebookId(notebookID string) string {
	serverID, _ := a.db.GetServerNotebookID(notebookID)
	return serverID
}

func (a *App) SetNotebookNotDirty(notebookID string) {
	a.db.SetNotebookNotDirty(notebookID)
}

func (a *App) SetNotebookNotDirtyNotDelete(notebookID string) {
	nb, _ := a.db.GetNotebook(notebookID)
	if nb != nil {
		nb.IsDirty = false
		nb.LocalIsDelete = false
		a.db.UpdateNotebook(nb)
	}
}

func (a *App) CountNotebooks(userID string) int {
	count, _ := a.db.CountNotebooks(userID)
	return count
}

func (a *App) CountAllNotes(userID string) int {
	count, _ := a.db.CountAllNotes(userID)
	return count
}

func (a *App) CountTags(userID string) int {
	count, _ := a.db.CountTags(userID)
	return count
}

func (a *App) ReCountNotebookNumberNotes(notebookID string) {
	a.db.CountNotes(notebookID)
}

func (a *App) HasNotesInNotebook(notebookID string) bool {
	has, _ := a.db.HasNotes(notebookID)
	return has
}

func (a *App) UpdateNoteUsn(noteID string, usn int64) {
	a.db.UpdateNoteUsn(noteID, usn)
}

func (a *App) UpdateTagCount(tag string) {
	user, _ := a.db.GetActiveUser()
	if user != nil {
		count, _ := a.db.CountNotesByTag(user.ID, tag)
		a.db.UpdateTagCount(tag, count)
	}
}

func (a *App) UpdateNoteToDeleteTag(tag string) {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return
	}
	notes, _ := a.db.SearchNotesByTag(user.ID, tag)
	for _, note := range notes {
		newTags := make([]string, 0)
		for _, t := range note.Tags {
			if t != tag {
				newTags = append(newTags, t)
			}
		}
		note.Tags = newTags
		note.IsDirty = true
		a.db.UpdateNote(note)
	}
}

func (a *App) SetTagNotDirty(tag string) {
	a.db.SetTagNotDirty(tag)
}

func (a *App) CountNotesByTag(tag string) int {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return 0
	}
	count, _ := a.db.CountNotesByTag(user.ID, tag)
	return count
}

func (a *App) FixContentUrl(content string) string {
	return utils.FixNoteContent(content, "http://127.0.0.1:8912/api2", "leanote://file")
}

func (a *App) GetImageUrl(fileID string) string {
	return "leanote://file/getImage?fileId=" + fileID
}

func (a *App) GetAttachUrl(fileID string) string {
	return "leanote://file/getAttach?fileId=" + fileID
}

func (a *App) GetUserByEmail(email string) map[string]interface{} {
	user, err := a.db.GetUserByEmail(email)
	if err != nil || user == nil {
		return nil
	}
	return map[string]interface{}{
		"UserId":   user.ID,
		"Username": user.Username,
		"Email":    user.Email,
		"IsLocal":  user.IsLocal,
	}
}

func (a *App) UpdateUserPwd(userID, password string) {
	hashedPwd := utils.MD5WithSalt(password, userID)
	a.db.UpdateUserPwd(userID, hashedPwd)
}

func (a *App) SetUserHost(userID, host string) {
	a.db.UpdateUserHost(userID, host)
}

// ==================== Missing Features Implementation ====================

// 4. updateAllBeLocal - 将远程账户转为本地账户
func (a *App) UpdateAllBeLocal(email, pwd, host string) map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return map[string]interface{}{"Ok": false, "Msg": "no active user"}
	}

	user.IsLocal = true
	user.Host = ""
	user.Token = ""
	a.db.UpdateUser(user)

	a.db.MarkAllDataAsLocal(user.ID)

	return map[string]interface{}{"Ok": true, "UserId": user.ID}
}

// 5. deleteUserAndAllData - 完整删除用户及其所有数据
func (a *App) DeleteUserAndAllData(userID string) map[string]interface{} {
	if user, _ := a.db.GetUser(userID); user != nil && user.Host != "" {
		accountID := db.SharedAccountID(user.Host, user.ID)
		a.db.DeleteSharedAccountRows(accountID)
		os.RemoveAll(filepath.Join(a.files.GetDataDir(), "shared", accountID))
	}

	a.files.DeleteUserDir(userID)

	a.db.DeleteAllNotes(userID)
	a.db.DeleteAllNotebooks(userID)
	a.db.DeleteAllTags(userID)
	a.db.DeleteAllImages(userID)
	a.db.DeleteAllAttachs(userID)
	a.db.DeleteAllNoteHistories(userID)
	a.db.DeleteUser(userID)

	return map[string]interface{}{"Ok": true}
}

// 7. deleteNotExistsAttach - 清理孤立附件
func (a *App) DeleteNotExistsAttach(noteID string, attachs []map[string]interface{}) {
	currentAttachs, _ := a.db.GetAttachsByNote(noteID)
	currentMap := make(map[string]bool)
	for _, att := range attachs {
		if fid, ok := att["FileId"].(string); ok {
			currentMap[fid] = true
		}
	}

	for _, att := range currentAttachs {
		if !currentMap[att.FileID] {
			if att.Path != "" {
				os.Remove(att.Path)
			}
			a.db.DeleteAttach(att.FileID)
		}
	}
}

// 8. writeBase64 - 通用 base64 写入
func (a *App) WriteBase64(data string, isImage bool, fileType string, title string) map[string]interface{} {
	result, err := a.files.WriteBase64(data, isImage, fileType, title)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

// 9. copyOtherSiteImage - 下载外站图片并入库
func (a *App) CopyOtherSiteImage(url string) map[string]interface{} {
	result, err := a.files.CopyOtherSiteImage(url)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}
	return result
}

// 10. deleteImages - 删除图片 (与 Electron 相同，为 no-op)
func (a *App) DeleteImages(noteID string) {
	// Intentionally no-op - images may be referenced by other notes
}

// 11. updateAttach - 更新笔记附件列表
func (a *App) UpdateAttach(noteID string, attachs []map[string]interface{}) map[string]interface{} {
	a.DeleteNotExistsAttach(noteID, attachs)

	user, _ := a.db.GetActiveUser()
	if user == nil {
		return map[string]interface{}{"Ok": false, "Msg": "no user"}
	}

	for _, att := range attachs {
		fileID, _ := att["FileId"].(string)
		title, _ := att["Title"].(string)
		attType, _ := att["Type"].(string)
		path, _ := att["Path"].(string)

		existing, _ := a.db.GetAttach(fileID)
		if existing == nil {
			now := time.Now()
			newAttach := &models.Attach{
				ID:          utils.ObjectId(),
				FileID:      fileID,
				NoteID:      noteID,
				UserID:      user.ID,
				Title:       title,
				Type:        attType,
				Path:        path,
				IsAttach:    true,
				IsDirty:     true,
				CreatedTime: &now,
			}
			a.db.InsertAttach(newAttach)
		}
	}

	return map[string]interface{}{"Ok": true}
}

// 13. getFileJson - 读取 JSON 文件
func (a *App) GetFileJson(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// 15. Version - 版本号
var AppVersion = api.ClientVersion

func (a *App) GetVersion() string {
	return AppVersion
}

// 16. Local PDF generation using gopdf
func (a *App) ExportPDF(noteID string) map[string]interface{} {
	note, err := a.db.GetNote(noteID)
	if err != nil || note == nil {
		return map[string]interface{}{"Ok": false, "Msg": "note not found"}
	}

	user, _ := a.db.GetActiveUser()
	exportDir := a.files.GetDataDir()
	if user != nil {
		exportDir = filepath.Join(a.files.GetUserDir(user.ID), "export")
	}
	os.MkdirAll(exportDir, 0755)

	pdfPath := filepath.Join(exportDir, noteID+".pdf")

	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()

	pdf.SetFont("Helvetica", "", 14)
	pdf.Cell(nil, note.Title)
	pdf.Br(20)

	pdf.SetFont("Helvetica", "", 11)
	content := note.Content
	if note.IsMarkdown {
		content = stripMarkdownTags(content)
	}

	lines, _ := pdf.SplitText(content, 500)
	for _, line := range lines {
		pdf.Cell(nil, line)
		pdf.Br(14)
	}

	if err := pdf.WritePdf(pdfPath); err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	return map[string]interface{}{
		"Ok":   true,
		"Path": pdfPath,
	}
}

func stripMarkdownTags(content string) string {
	content = strings.ReplaceAll(content, "#", "")
	content = strings.ReplaceAll(content, "*", "")
	content = strings.ReplaceAll(content, "_", "")
	content = strings.ReplaceAll(content, "`", "")
	return content
}

// 17. Show/Hide window for tray
func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
		runtime.WindowSetAlwaysOnTop(a.ctx, true)
		runtime.WindowSetAlwaysOnTop(a.ctx, false)
	}
}

func (a *App) HideWindow() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
}

// Deep link handling
func (a *App) HandleDeepLink(url string) map[string]interface{} {
	if strings.HasPrefix(url, "leanote://") {
		if strings.Contains(url, "note/") {
			parts := strings.Split(url, "/")
			if len(parts) >= 3 {
				noteID := parts[len(parts)-1]
				return map[string]interface{}{"Action": "openNote", "NoteId": noteID}
			}
		} else if strings.Contains(url, "notebook/") {
			parts := strings.Split(url, "/")
			if len(parts) >= 3 {
				notebookID := parts[len(parts)-1]
				return map[string]interface{}{"Action": "openNotebook", "NotebookId": notebookID}
			}
		}
	}
	return map[string]interface{}{"Action": "none"}
}

// Get all attachs for cleanup
func (a *App) GetAllAttachs() []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}

	attachs, err := a.db.GetAllAttachs(user.ID)
	if err != nil {
		return nil
	}

	result := make([]map[string]interface{}, len(attachs))
	for i, att := range attachs {
		result[i] = map[string]interface{}{
			"FileId":  att.FileID,
			"NoteId":  att.NoteID,
			"Title":   att.Title,
			"Path":    att.Path,
			"IsDirty": att.IsDirty,
		}
	}
	return result
}

// Get all images for cleanup
func (a *App) GetAllImages() []map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return nil
	}

	images, err := a.db.GetAllImages(user.ID)
	if err != nil {
		return nil
	}

	result := make([]map[string]interface{}, len(images))
	for i, img := range images {
		result[i] = map[string]interface{}{
			"FileId":       img.FileID,
			"Path":         img.Path,
			"ServerFileId": img.ServerFileID,
			"IsDirty":      img.IsDirty,
		}
	}
	return result
}

// Rebuild image index
func (a *App) RebuildImageIndex() map[string]interface{} {
	user, _ := a.db.GetActiveUser()
	if user == nil {
		return map[string]interface{}{"Ok": false, "Msg": "no user"}
	}

	imageDir := a.files.GetUserImageDir(user.ID)
	entries, err := os.ReadDir(imageDir)
	if err != nil {
		return map[string]interface{}{"Ok": false, "Msg": err.Error()}
	}

	added := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if !utils.IsImageExt(strings.TrimPrefix(ext, ".")) {
			continue
		}

		fileID := strings.TrimSuffix(name, ext)
		existing, _ := a.db.GetImage(fileID)
		if existing == nil {
			fullPath := filepath.Join(imageDir, name)
			now := time.Now()
			img := &models.Image{
				ID:          utils.ObjectId(),
				FileID:      fileID,
				UserID:      user.ID,
				Path:        fullPath,
				IsDirty:     false,
				CreatedTime: &now,
			}
			a.db.InsertImage(img)
			added++
		}
	}

	return map[string]interface{}{"Ok": true, "Added": added}
}
