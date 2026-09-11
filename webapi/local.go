package webapi

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

var (
	legacyImageRe  = regexp.MustCompile(`leanote://file/getImage\?fileId=([a-zA-Z0-9]{24})`)
	legacyAttachRe = regexp.MustCompile(`leanote://file/getAttach\?fileId=([a-zA-Z0-9]{24})`)
	htmlTagRe      = regexp.MustCompile(`<[^>]*>`)
	mdMarkerRe     = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)|[#>*_\x60~]+`)
	spaceRe        = regexp.MustCompile(`\s+`)
)

func normalizeContent(s string) string {
	s = legacyImageRe.ReplaceAllString(s, "/api/file/getImage?fileId=$1")
	return legacyAttachRe.ReplaceAllString(s, "/api/file/getAttach?fileId=$1")
}

func excerpt(content string, limit int) string {
	text := mdMarkerRe.ReplaceAllString(content, " ")
	text = htmlTagRe.ReplaceAllString(text, " ")
	text = spaceRe.ReplaceAllString(strings.TrimSpace(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func splitTags(s string) []string {
	var tags []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func timeOrNow(t *time.Time) *time.Time {
	if t == nil {
		now := time.Now()
		return &now
	}
	return t
}

func formList(r *http.Request, key string) []string {
	var values []string
	for formKey, vals := range r.Form {
		if formKey == key || (strings.HasPrefix(formKey, key+"[") && strings.HasSuffix(formKey, "]")) {
			values = append(values, vals...)
		}
	}
	sort.Strings(values)
	return values
}

func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 32 {
			return '_'
		}
		return r
	}, s)
}

func (h *Handler) activeUser() *models.User {
	user, err := h.DB.GetActiveUser()
	if err != nil {
		return nil
	}
	return user
}

func (h *Handler) requireUser(w http.ResponseWriter) *models.User {
	user := h.activeUser()
	if user == nil {
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": "NOTLOGIN"})
	}
	return user
}

func (h *Handler) sharedCacheState(user *models.User) string {
	if user.Host == "" {
		return ""
	}
	return h.DB.SharedCapabilityState(db.SharedAccountID(user.Host, user.ID))
}

func (h *Handler) userLogo(userID string) string {
	logo, _ := h.DB.GetConfig("logo:" + userID)
	return logo
}

func (h *Handler) bootstrap(w http.ResponseWriter) {
	user := h.activeUser()
	if user == nil {
		openRegister, needCaptcha := false, false
		if h.Proxy != nil {
			openRegister, needCaptcha = h.Proxy.GuestConfig()
		}
		host, _ := h.DB.GetConfig("host")
		h.writeJSON(w, map[string]any{"Ok": true, "User": nil, "OpenRegister": openRegister, "NeedCaptcha": needCaptcha, "Desktop": true, "Host": host})
		return
	}

	notebooks, err := h.DB.GetNotebooks(user.ID)
	if err != nil {
		notebooks = nil
	}
	tags, err := h.DB.GetTags(user.ID)
	if err != nil {
		tags = nil
	}
	if tags == nil {
		tags = []*models.Tag{}
	}
	totalNotes, err := h.DB.CountAllNotes(user.ID)
	if err != nil {
		totalNotes = 0
	}

	shared := map[string]any{}
	isAdmin := false
	if user.Token != "" && !user.IsLocal {
		accountID := db.SharedAccountID(user.Host, user.ID)
		if cached, cacheErr := h.DB.SharedNotebooks(accountID); cacheErr == nil {
			shared = cached
		}
	}
	if h.Proxy != nil {
		_, isAdmin = h.Proxy.SharedNotebooks(user)
	}

	h.writeJSON(w, map[string]any{
		"Ok":              true,
		"User":            map[string]any{"UserId": user.ID, "Username": user.Username, "Email": user.Email, "Logo": h.userLogo(user.ID)},
		"IsAdmin":         isAdmin,
		"Notebooks":       h.DB.MapNotebooks(notebooks),
		"SharedNotebooks": shared,
		"Tags":            tags,
		"TotalNotes":      totalNotes,
		"Version":         h.Version,
		"SharedCache":     h.sharedCacheState(user),
	})
}

func (h *Handler) noteListItem(n *models.Note) map[string]any {
	return map[string]any{
		"NoteId":      n.NoteID,
		"NotebookId":  n.NotebookID,
		"Title":       n.Title,
		"Desc":        n.Desc,
		"CreatedTime": timeOrNow(n.CreatedTime),
		"UpdatedTime": timeOrNow(n.UpdatedTime),
	}
}

func (h *Handler) documentNote(n *models.Note) map[string]any {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"NoteId":      n.NoteID,
		"NotebookId":  n.NotebookID,
		"UserId":      n.UserID,
		"Title":       n.Title,
		"Tags":        tags,
		"Usn":         n.Usn,
		"IsMarkdown":  n.IsMarkdown,
		"IsTrash":     n.IsTrash,
		"CreatedTime": timeOrNow(n.CreatedTime),
		"UpdatedTime": timeOrNow(n.UpdatedTime),
	}
}

func (h *Handler) respondDocument(w http.ResponseWriter, noteID, userID string) {
	note, err := h.DB.GetNote(noteID)
	if err != nil || note == nil {
		h.fail(w, "notExists")
		return
	}
	h.writeJSON(w, map[string]any{
		"Note":     h.documentNote(note),
		"Content":  normalizeContent(note.Content),
		"Writable": note.UserID == userID,
	})
}

func (h *Handler) document(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	noteID := h.form(r, "noteId")
	accountID := db.SharedAccountID(user.Host, user.ID)
	if shared, err := h.DB.GetSharedNote(accountID, noteID); err == nil && shared != nil {
		if shared.CacheState == "pending" || shared.Content == "" && shared.CachedContentVersion == "" {
			h.fail(w, "sharedNotCached")
			return
		}
		h.writeJSON(w, map[string]any{"Note": map[string]any{"NoteId": shared.NoteID, "NotebookId": shared.NotebookID, "UserId": shared.OwnerUserID, "OwnerUserId": shared.OwnerUserID, "Title": shared.Title, "Tags": shared.Tags, "Usn": 0, "IsMarkdown": shared.IsMarkdown, "IsTrash": false, "IsShared": true, "Perm": shared.Perm, "CachedAt": shared.CachedAt, "CacheState": shared.CacheState, "CreatedTime": timeOrNow(shared.CreatedTime), "UpdatedTime": timeOrNow(shared.UpdatedTime)}, "Content": normalizeContent(shared.Content), "Writable": false})
		return
	}
	h.respondDocument(w, noteID, user.ID)
}

func (h *Handler) sharedNotes(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	if user.Token == "" {
		h.writeJSON(w, []any{})
		return
	}
	accountID := db.SharedAccountID(user.Host, user.ID)
	list, err := h.DB.ListSharedNotes(accountID, h.form(r, "userId"), h.form(r, "notebookId"), h.form(r, "key"))
	if err != nil {
		h.fail(w, err.Error())
		return
	}
	sortField := h.form(r, "sortField")
	sort.SliceStable(list, func(i, j int) bool {
		switch sortField {
		case "Title":
			return strings.ToLower(list[i].Title) < strings.ToLower(list[j].Title)
		case "CreatedTime":
			return timeOrNow(list[i].CreatedTime).After(*timeOrNow(list[j].CreatedTime))
		default:
			return timeOrNow(list[i].UpdatedTime).After(*timeOrNow(list[j].UpdatedTime))
		}
	})
	page, _ := strconv.Atoi(h.form(r, "page"))
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start > len(list) {
		start = len(list)
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	items := make([]map[string]any, 0, end-start)
	for _, n := range list[start:end] {
		items = append(items, map[string]any{"NoteId": n.NoteID, "NotebookId": n.NotebookID, "UserId": n.OwnerUserID, "Title": n.Title, "Desc": n.Desc, "Perm": n.Perm, "IsShared": true, "CacheState": n.CacheState, "CreatedTime": timeOrNow(n.CreatedTime), "UpdatedTime": timeOrNow(n.UpdatedTime)})
	}
	h.writeJSON(w, items)
}

func (h *Handler) rejectSharedWrite(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method == http.MethodGet {
		return false
	}
	write := map[string]bool{"/web/save": true, "/web/restore": true, "/note/deleteNote": true, "/note/deleteTrash": true, "/note/moveNote": true, "/note/copyNote": true, "/attach/uploadAttach": true, "/attach/deleteAttach": true, "/file/pasteImage": true}
	if !write[path] {
		return false
	}
	user := h.activeUser()
	if user == nil {
		return false
	}
	accountID := db.SharedAccountID(user.Host, user.ID)
	noteID := h.form(r, "noteId")
	if noteID != "" && h.DB.IsSharedNote(accountID, noteID) {
		h.fail(w, "sharedReadOnly")
		return true
	}
	for _, id := range formList(r, "noteIds") {
		if h.DB.IsSharedNote(accountID, id) {
			h.fail(w, "sharedReadOnly")
			return true
		}
	}
	if path == "/web/save" && h.form(r, "ownerId") != "" && h.form(r, "ownerId") != user.ID {
		h.fail(w, "sharedReadOnly")
		return true
	}
	if notebookID := h.form(r, "notebookId"); notebookID != "" && h.DB.IsSharedNotebook(accountID, notebookID) {
		h.fail(w, "sharedReadOnly")
		return true
	}
	if path == "/attach/deleteAttach" && h.DB.IsSharedFile(accountID, h.form(r, "attachId")) {
		h.fail(w, "sharedReadOnly")
		return true
	}
	return false
}

func (h *Handler) sortedNotes(user *models.User, notebookID, key, tag, sortField string, trash bool) ([]*models.Note, error) {
	var (
		list []*models.Note
		err  error
	)
	switch {
	case trash:
		list, err = h.DB.GetTrashNotes(user.ID)
	case key != "":
		list, err = h.DB.SearchNotes(user.ID, key)
	case tag != "":
		list, err = h.DB.SearchNotesByTag(user.ID, tag)
	case notebookID != "":
		list, err = h.DB.GetNotes(notebookID)
	default:
		list, err = h.DB.GetAllNotes(user.ID)
	}
	if err != nil {
		return nil, err
	}

	switch sortField {
	case "Title":
		sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Title) < strings.ToLower(list[j].Title) })
	case "CreatedTime":
		sort.SliceStable(list, func(i, j int) bool {
			return timeOrNow(list[i].CreatedTime).After(*timeOrNow(list[j].CreatedTime))
		})
	default:
		sort.SliceStable(list, func(i, j int) bool {
			return timeOrNow(list[i].UpdatedTime).After(*timeOrNow(list[j].UpdatedTime))
		})
	}
	return list, nil
}

func (h *Handler) notes(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	page, _ := strconv.Atoi(h.form(r, "page"))
	if page < 1 {
		page = 1
	}
	list, err := h.sortedNotes(user, h.form(r, "notebookId"), h.form(r, "key"), h.form(r, "tag"), h.form(r, "sort"), h.form(r, "trash") == "true")
	if err != nil {
		h.fail(w, err.Error())
		return
	}
	start := (page - 1) * pageSize
	if start > len(list) {
		start = len(list)
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	items := make([]map[string]any, 0, end-start)
	for _, n := range list[start:end] {
		items = append(items, h.noteListItem(n))
	}
	h.writeJSON(w, items)
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	noteID := h.form(r, "noteId")
	if noteID == "" {
		h.fail(w, "invalidId")
		return
	}
	title := h.form(r, "title")
	content := r.FormValue("content")
	tags := splitTags(h.form(r, "tags"))
	isNew := h.form(r, "isNew") == "true"
	isMarkdown := h.form(r, "isMarkdown") == "true"
	notebookID := h.form(r, "notebookId")

	if isNew {
		if notebookID == "" {
			h.fail(w, "invalidNotebook")
			return
		}
		if existing, _ := h.DB.GetNote(noteID); existing != nil {
			h.fail(w, "conflict")
			return
		}
		now := time.Now()
		ownerID := h.form(r, "ownerId")
		if ownerID == "" {
			ownerID = user.ID
		}
		note := &models.Note{
			ID:          utils.ObjectId(),
			NoteID:      noteID,
			NotebookID:  notebookID,
			UserID:      ownerID,
			Title:       title,
			Content:     content,
			Desc:        excerpt(content, 150),
			Tags:        tags,
			IsMarkdown:  isMarkdown,
			IsDirty:     true,
			LocalIsNew:  true,
			CreatedTime: &now,
			UpdatedTime: &now,
		}
		if err := h.DB.InsertNote(note); err != nil {
			h.fail(w, "saveFailed")
			return
		}
		h.touchTags(user.ID, tags)
		h.recountNotebook(notebookID)
		h.respondDocument(w, noteID, user.ID)
		return
	}

	note, err := h.DB.GetNote(noteID)
	if err != nil || note == nil || note.IsTrash {
		h.fail(w, "notExists")
		return
	}
	// Offline-first: the server-side usn conflict check is skipped; the sync
	// service resolves conflicts when the change is pushed.
	if note.Content != content {
		h.DB.AddNoteHistory(noteID, note.Content)
		note.Content = content
		note.ContentIsDirty = true
		note.Desc = excerpt(content, 150)
	}
	note.Title = title
	note.Tags = tags
	note.IsDirty = true
	if err := h.DB.UpdateNote(note); err != nil {
		h.fail(w, "saveFailed")
		return
	}
	h.touchTags(user.ID, tags)
	h.respondDocument(w, noteID, user.ID)
}

func (h *Handler) touchTags(userID string, tags []string) {
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, err := h.DB.GetTag(userID, tag); err != nil {
			h.DB.AddOrUpdateTag(userID, tag, false, 0)
		}
		if count, err := h.DB.CountNotesByTag(userID, tag); err == nil {
			h.DB.UpdateTagCount(tag, count)
		}
	}
}

func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	note, err := h.DB.GetNote(h.form(r, "noteId"))
	if err != nil || note == nil || !note.IsTrash {
		h.fail(w, "notExists")
		return
	}
	if err := h.DB.SetNoteTrash(note.NoteID, false); err != nil {
		h.fail(w, err.Error())
		return
	}
	h.recountNotebook(note.NotebookID)
	h.ok(w)
}

func (h *Handler) addNotebook(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	title := h.form(r, "title")
	if title == "" {
		h.fail(w, "noTitle")
		return
	}
	parentNotebookID := h.form(r, "parentNotebookId")
	if parentNotebookID != "" {
		parent, err := h.DB.GetNotebook(parentNotebookID)
		if err != nil || parent == nil || parent.UserID != user.ID || parent.LocalIsDelete {
			h.fail(w, "invalidParentNotebook")
			return
		}
	}
	now := time.Now()
	nb := &models.Notebook{
		ID:               utils.ObjectId(),
		NotebookID:       h.form(r, "notebookId"),
		ParentNotebookID: parentNotebookID,
		Title:            title,
		UserID:           user.ID,
		IsDirty:          true,
		LocalIsNew:       true,
		CreatedTime:      &now,
		UpdatedTime:      &now,
	}
	if err := h.DB.InsertNotebook(nb); err != nil {
		h.fail(w, err.Error())
		return
	}
	h.ok(w)
}

func (h *Handler) renameNotebook(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	nb, err := h.DB.GetNotebook(h.form(r, "notebookId"))
	if err != nil || nb == nil {
		h.fail(w, "notExists")
		return
	}
	nb.Title = h.form(r, "title")
	nb.IsDirty = true
	if err := h.DB.UpdateNotebook(nb); err != nil {
		h.fail(w, err.Error())
		return
	}
	h.ok(w)
}

func (h *Handler) deleteNotebook(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	notebookID := h.form(r, "notebookId")
	if has, _ := h.DB.HasNotes(notebookID); has {
		h.fail(w, "notebookHasNotes")
		return
	}
	if err := h.DB.DeleteNotebook(notebookID); err != nil {
		h.fail(w, err.Error())
		return
	}
	h.ok(w)
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	for _, noteID := range formList(r, "noteIds") {
		if note, err := h.DB.GetNote(noteID); err == nil && note != nil {
			h.DB.SetNoteTrash(noteID, true)
			h.recountNotebook(note.NotebookID)
		}
	}
	h.writeJSON(w, true)
}

func (h *Handler) deleteTrashNote(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	noteID := h.form(r, "noteId")
	if note, err := h.DB.GetNote(noteID); err == nil && note != nil {
		h.DB.MarkNoteLocalDelete(noteID)
		h.Files.DeleteNoteFiles(noteID)
		h.DB.DeleteNoteHistories(noteID)
		h.recountNotebook(note.NotebookID)
	}
	h.writeJSON(w, true)
}

func (h *Handler) moveNote(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	for _, noteID := range formList(r, "noteIds") {
		note, err := h.DB.GetNote(noteID)
		if err != nil || note == nil {
			continue
		}
		oldNotebookID := note.NotebookID
		h.DB.MoveNote(noteID, h.form(r, "notebookId"))
		h.recountNotebook(oldNotebookID)
		h.recountNotebook(h.form(r, "notebookId"))
	}
	h.writeJSON(w, true)
}

func (h *Handler) recountNotebook(notebookID string) {
	if count, err := h.DB.CountNotes(notebookID); err == nil {
		h.DB.UpdateNotebookNumberNotes(notebookID, count)
	}
}

func (h *Handler) copyNote(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	copied := []string{}
	for _, noteID := range formList(r, "noteIds") {
		if note, err := h.DB.CopyNote(noteID, h.form(r, "notebookId")); err == nil && note != nil {
			copied = append(copied, note.NoteID)
			h.recountNotebook(h.form(r, "notebookId"))
		}
	}
	h.writeJSON(w, map[string]any{"Ok": true, "Item": copied})
}

func (h *Handler) listHistories(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	if user.Host != "" {
		accountID := db.SharedAccountID(user.Host, user.ID)
		if h.DB.IsSharedNote(accountID, h.form(r, "noteId")) {
			h.fail(w, "sharedHistoryUnsupported")
			return
		}
	}
	histories, err := h.DB.GetNoteHistories(h.form(r, "noteId"))
	if err != nil {
		histories = nil
	}
	items := []map[string]any{}
	for _, hist := range histories {
		items = append(items, map[string]any{
			"UpdatedTime": timeOrNow(hist.UpdatedTime),
			"Content":     normalizeContent(hist.Content),
		})
	}
	h.writeJSON(w, items)
}

func (h *Handler) attachItem(a *models.Attach) map[string]any {
	size := int64(0)
	if a.Path != "" {
		if info, err := os.Stat(a.Path); err == nil {
			size = info.Size()
		}
	}
	return map[string]any{
		"AttachId":    a.FileID,
		"Title":       a.Title,
		"Name":        a.Title,
		"Type":        a.Type,
		"Size":        size,
		"CreatedTime": timeOrNow(a.CreatedTime),
	}
}

func (h *Handler) getAttachs(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	if user.Host != "" {
		accountID := db.SharedAccountID(user.Host, user.ID)
		if h.DB.IsSharedNote(accountID, h.form(r, "noteId")) {
			files, err := h.DB.ListSharedAttachmentsForNote(accountID, h.form(r, "noteId"))
			if err != nil {
				h.fail(w, err.Error())
				return
			}
			list := []map[string]any{}
			for _, f := range files {
				list = append(list, map[string]any{
					"AttachId": f.FileID, "Title": f.Title, "Name": f.Title, "Type": f.Kind,
					"Size": f.Size, "CacheState": f.CacheState, "CachedAt": timeOrNow(f.CachedAt),
				})
			}
			h.writeJSON(w, map[string]any{"Ok": true, "List": list})
			return
		}
	}
	attachs, err := h.Files.GetAttachsByNote(h.form(r, "noteId"))
	if err != nil {
		attachs = nil
	}
	list := []map[string]any{}
	for _, a := range attachs {
		list = append(list, h.attachItem(a))
	}
	h.writeJSON(w, map[string]any{"Ok": true, "List": list})
}

func saveMultipartFile(fh *multipart.FileHeader, dir string) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	ext := filepath.Ext(fh.Filename)
	tmp, err := os.CreateTemp("", "gemsnote-upload-*"+ext)
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := tmp.ReadFrom(src); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	tmpName := tmp.Name()
	target := filepath.Join(dir, "upload-"+filepath.Base(tmpName)+ext)
	if err := os.Rename(tmpName, target); err != nil {
		data, readErr := os.ReadFile(tmpName)
		if readErr != nil {
			os.Remove(tmpName)
			return "", readErr
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			os.Remove(tmpName)
			return "", err
		}
		os.Remove(tmpName)
	}
	return target, nil
}

func (h *Handler) uploadAttach(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	_, fh, err := r.FormFile("file")
	if err != nil {
		h.fail(w, "noFile")
		return
	}
	attach, err := h.storeUpload(fh, h.form(r, "noteId"))
	if err != nil {
		h.fail(w, err.Error())
		return
	}
	h.writeJSON(w, map[string]any{"Ok": true, "Id": attach.FileID})
}

func (h *Handler) storeUpload(fh *multipart.FileHeader, noteID string) (*models.Attach, error) {
	user, err := h.DB.GetActiveUser()
	if err != nil || user == nil {
		return nil, fmt.Errorf("no active user")
	}
	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	fileID := utils.ObjectId()
	ext := strings.TrimPrefix(filepath.Ext(fh.Filename), ".")
	target := filepath.Join(h.Files.GetUserAttachDir(user.ID), utils.UUID()+"."+ext)
	if err := os.MkdirAll(h.Files.GetUserAttachDir(user.ID), 0755); err != nil {
		return nil, err
	}
	dst, err := os.Create(target)
	if err != nil {
		return nil, err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(target)
		return nil, err
	}

	now := time.Now()
	attach := &models.Attach{
		ID:          utils.ObjectId(),
		FileID:      fileID,
		NoteID:      noteID,
		UserID:      user.ID,
		Title:       filepath.Base(fh.Filename),
		Type:        ext,
		Path:        target,
		IsAttach:    true,
		IsDirty:     true,
		CreatedTime: &now,
	}
	if err := h.DB.InsertAttach(attach); err != nil {
		os.Remove(target)
		return nil, err
	}
	return attach, nil
}

func (h *Handler) deleteAttach(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	if err := h.Files.DeleteAttach(h.form(r, "attachId")); err != nil {
		h.fail(w, err.Error())
		return
	}
	h.ok(w)
}

func (h *Handler) downloadAttach(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	var path, title string
	attachID := h.form(r, "attachId")
	if user.Host != "" {
		accountID := db.SharedAccountID(user.Host, user.ID)
		if h.DB.IsSharedFile(accountID, attachID) {
			path, title, _, _ = h.DB.GetSharedFilePath(accountID, attachID, "attachment")
		} else {
			path, title, _ = h.Files.GetAttach(attachID)
		}
	} else {
		path, title, _ = h.Files.GetAttach(attachID)
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", sanitizeFilename(title), url.PathEscape(title)))
	http.ServeFile(w, r, path)
}

func (h *Handler) pasteImage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	_, fh, err := r.FormFile("file")
	if err != nil {
		h.fail(w, "noFile")
		return
	}
	tmp, err := saveMultipartFile(fh, os.TempDir())
	if err != nil {
		h.fail(w, err.Error())
		return
	}
	defer os.Remove(tmp)
	result, err := h.Files.CopyFile(tmp, true)
	if err != nil {
		h.fail(w, err.Error())
		return
	}
	h.writeJSON(w, map[string]any{"Ok": true, "Id": result["FileId"]})
}

func (h *Handler) serveImage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	var path string
	fileID := h.form(r, "fileId")
	if user.Host != "" {
		accountID := db.SharedAccountID(user.Host, user.ID)
		if h.DB.IsSharedFile(accountID, fileID) {
			path, _, _, _ = h.DB.GetSharedFilePath(accountID, fileID, "image")
		} else {
			path, _ = h.Files.GetImage(fileID)
		}
	} else {
		path, _ = h.Files.GetImage(fileID)
	}
	if path == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	http.ServeFile(w, r, path)
}

func (h *Handler) doLogin(w http.ResponseWriter, r *http.Request) {
	email := h.form(r, "email")
	pwd := r.FormValue("pwd")
	if email == "" || pwd == "" {
		h.fail(w, "invalidParams")
		return
	}

	// A configured server is always authoritative for remote login. Do not
	// reuse a local user with the same username/email: that would show the old
	// server's cache without validating credentials against the new server.
	if h.Proxy != nil && h.Proxy.configured() {
		if ok, msg := h.Proxy.LoginServer(email, pwd); ok {
			h.adoptServerUser(email, pwd)
			h.fireLoginHook()
			result := map[string]any{"Ok": true}
			if notice := h.Proxy.VersionNotice(); notice != "" {
				result["Notice"] = notice
			}
			h.writeJSON(w, result)
			return
		} else {
			if msg == "offline" {
				h.fail(w, "offline")
			} else {
				h.fail(w, msg)
			}
			return
		}
	}

	// Only explicitly local accounts may authenticate from the local cache.
	user, _ := h.DB.GetUserByNameOrEmail(email)
	if user != nil && user.IsLocal && user.Host == "" {
		if msg := h.verifyLocalPassword(user, pwd); msg != "" {
			h.fail(w, msg)
			return
		}
		h.DB.SwitchUser(user.ID)
		h.DB.SetCurrentUser(user.ID)
		h.Files.InitUserDirs(user.ID)
		h.DB.UpdateLastLoginTime(user.ID)
		h.fireLoginHook()
		h.writeJSON(w, map[string]any{"Ok": true})
		return
	}

	h.fail(w, "userNotExist")
}

func (h *Handler) adoptServerUser(email, pwd string) {
	serverUser := h.Proxy.fetchServerUser()
	if serverUser == nil {
		return
	}
	host, _ := h.DB.GetConfig("host")
	serverUser.Host = host
	serverUser.Pwd = utils.MD5WithSalt(pwd, serverUser.ID)
	if existing, _ := h.DB.GetUser(serverUser.ID); existing != nil {
		h.DB.UpdateUser(serverUser)
	} else {
		h.DB.InsertUser(serverUser)
	}
	h.DB.SwitchUser(serverUser.ID)
	h.DB.SetCurrentUser(serverUser.ID)
	h.Files.InitUserDirs(serverUser.ID)
	if token := h.Proxy.FetchAPIToken(email, pwd); token != "" {
		h.DB.UpdateUserToken(serverUser.ID, token)
	}
	h.DB.SetConfig("proxy:email", email)
	h.DB.SetConfig("proxy:pwd", pwd)
}

func (h *Handler) fireLoginHook() {
	if h.OnLogin != nil {
		go h.OnLogin()
	}
}

func (h *Handler) verifyLocalPassword(user *models.User, password string) string {
	if user.Pwd == "" {
		return ""
	}
	hashed := utils.MD5WithSalt(password, user.ID)
	if len(user.Pwd) == 32 {
		if user.Pwd != hashed {
			return "password incorrect"
		}
		return ""
	}
	if user.Pwd != password {
		return "password incorrect"
	}
	h.DB.UpdateUserPwd(user.ID, hashed)
	return ""
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.OnLogout != nil {
		if err := h.OnLogout(); err != nil {
			h.fail(w, "syncFailed")
			return
		}
	}
	if h.Proxy != nil {
		h.Proxy.Logout()
	}
	if user := h.activeUser(); user != nil {
		h.DB.UpdateUserToken(user.ID, "")
	}
	h.DB.DeactivateAllUsers()
	h.DB.SetCurrentUser("")
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (h *Handler) queueSharedDownload(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w)
	if user == nil {
		return
	}
	if user.Host == "" {
		h.fail(w, "sharedCacheUnavailable")
		return
	}
	accountID := db.SharedAccountID(user.Host, user.ID)
	n, err := h.DB.QueueSharedAttachment(accountID, h.form(r, "attachId"))
	if err != nil {
		h.fail(w, err.Error())
		return
	}
	if n == 0 {
		h.fail(w, "notFound")
		return
	}
	if h.OnSharedDownload != nil {
		h.OnSharedDownload()
	}
	h.ok(w)
}
