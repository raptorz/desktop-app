package webapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/service"
	"github.com/gemsnote/gemsnote/utils"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testEnv struct {
	handler *Handler
	db      *db.Database
	tmp     string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	tmp := t.TempDir()
	files := service.NewFileService(database)
	files.SetDataDir(tmp)
	dist := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html>spa</html>")}}
	handler := &Handler{DB: database, Files: files, Version: "test", Dist: dist}
	return &testEnv{handler: handler, db: database, tmp: tmp}
}

func (e *testEnv) get(t *testing.T, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (e *testEnv) post(t *testing.T, path string, form url.Values) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (e *testEnv) postJSON(t *testing.T, path string, form url.Values, out any) {
	t.Helper()
	_, body := e.post(t, path, form)
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("%s: invalid JSON %s: %v", path, body, err)
	}
}

func (e *testEnv) login(t *testing.T) (userID, notebookID string) {
	t.Helper()
	userID = utils.ObjectId()
	hashed := utils.MD5WithSalt("secret", userID)
	now := time.Now()
	if err := e.db.InsertUser(&models.User{ID: userID, Username: "tester", Email: "t@gemsnote.test", Pwd: hashed, IsActive: true, IsLocal: true, CreatedTime: &now}); err != nil {
		t.Fatal(err)
	}
	e.db.SetCurrentUser(userID)
	e.handler.Files.InitUserDirs(userID)

	notebookID = utils.ObjectId()
	nbTime := time.Now()
	if err := e.db.InsertNotebook(&models.Notebook{ID: utils.ObjectId(), NotebookID: notebookID, Title: "默认笔记本", UserID: userID, CreatedTime: &nbTime, UpdatedTime: &nbTime}); err != nil {
		t.Fatal(err)
	}
	return userID, notebookID
}

func TestGuestBootstrapAndNotLogin(t *testing.T) {
	e := newTestEnv(t)

	_, body := e.get(t, "/web/bootstrap")
	var guest struct {
		Ok      bool
		User    any
		Desktop bool
	}
	json.Unmarshal(body, &guest)
	if !guest.Ok || guest.User != nil || !guest.Desktop {
		t.Fatalf("guest bootstrap mismatch: %s", body)
	}

	_, body = e.post(t, "/web/notes", url.Values{})
	if !strings.Contains(string(body), "NOTLOGIN") {
		t.Fatalf("expected NOTLOGIN, got %s", body)
	}
}

func TestSyncEndpoints(t *testing.T) {
	e := newTestEnv(t)

	_, body := e.post(t, "/web/sync", url.Values{})
	if !strings.Contains(string(body), "NOTLOGIN") {
		t.Fatalf("expected NOTLOGIN, got %s", body)
	}

	e.login(t)
	synced, fullSynced := false, false
	e.handler.OnSync = func() (any, error) {
		synced = true
		return map[string]any{"Ok": true, "Note": map[string]any{"Adds": 3}}, nil
	}
	e.handler.OnFullSync = func() (any, error) {
		fullSynced = true
		return nil, fmt.Errorf("offline")
	}

	_, body = e.get(t, "/web/sync")
	if synced || !strings.Contains(string(body), "notFound") {
		t.Fatalf("GET /web/sync must not invoke hook: %s", body)
	}
	_, body = e.get(t, "/web/fullSync")
	if fullSynced || !strings.Contains(string(body), "notFound") {
		t.Fatalf("GET /web/fullSync must not invoke hook: %s", body)
	}

	e.handler.OnSync = nil
	_, body = e.post(t, "/web/sync", url.Values{})
	if !strings.Contains(string(body), "unsupported") {
		t.Fatalf("expected unsupported without hook, got %s", body)
	}

	e.handler.OnSync = func() (any, error) {
		synced = true
		return map[string]any{"Ok": true, "Note": map[string]any{"Adds": 3}}, nil
	}
	e.handler.OnFullSync = func() (any, error) {
		fullSynced = true
		return nil, fmt.Errorf("offline")
	}

	_, body = e.post(t, "/web/sync", url.Values{})
	if !synced || !strings.Contains(string(body), `"Adds":3`) {
		t.Fatalf("incremental sync hook not invoked or wrong body: %s", body)
	}

	_, body = e.post(t, "/web/fullSync", url.Values{})
	if !fullSynced || !strings.Contains(string(body), "offline") {
		t.Fatalf("full sync hook not invoked or wrong body: %s", body)
	}
}

func TestSharedBatchWritesAreRejected(t *testing.T) {
	e := newTestEnv(t)
	userID, notebookID := e.login(t)
	user, _ := e.db.GetUser(userID)
	user.Host = "https://notes.example"
	user.Token = "token"
	if err := e.db.UpdateUser(user); err != nil {
		t.Fatal(err)
	}
	accountID := db.SharedAccountID(user.Host, userID)
	sharedNoteID := utils.ObjectId()
	if err := e.db.PublishSharedSnapshot(accountID, []models.SharedSnapshotItem{{Kind: "note", Note: &models.SharedNote{
		NoteID: sharedNoteID, OwnerUserID: utils.ObjectId(), TargetContentVersion: "v1",
	}}}, 1); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/note/moveNote", "/note/copyNote", "/note/deleteNote"} {
		_, body := e.post(t, path, url.Values{"noteIds[0]": {sharedNoteID}, "notebookId": {notebookID}})
		if !strings.Contains(string(body), "sharedReadOnly") {
			t.Fatalf("%s accepted shared write: %s", path, body)
		}
	}
}

func TestNoteLifecycleRoundtrip(t *testing.T) {
	e := newTestEnv(t)
	userID, notebookID := e.login(t)

	var loginResp map[string]any
	e.postJSON(t, "/doLogin", url.Values{"email": {"tester"}, "pwd": {"secret"}}, &loginResp)
	if loginResp["Ok"] != true {
		t.Fatalf("login failed: %v", loginResp)
	}

	var boot struct {
		Desktop   bool
		Notebooks []struct {
			NotebookId string
			Title      string
		}
		Tags []map[string]any
	}
	_, body := e.get(t, "/web/bootstrap")
	json.Unmarshal(body, &boot)
	if !boot.Desktop {
		t.Fatalf("authenticated bootstrap must identify desktop: %s", body)
	}
	if len(boot.Notebooks) != 1 || boot.Notebooks[0].NotebookId != notebookID {
		t.Fatalf("bootstrap notebooks mismatch: %s", body)
	}

	noteID := utils.ObjectId()
	var doc struct {
		Note struct {
			NoteId     string
			NotebookId string
			UserId     string
			Tags       []string
			Usn        int
			IsMarkdown bool
		}
		Content  string
		Writable bool
	}
	e.postJSON(t, "/web/save", url.Values{
		"noteId":     {noteID},
		"notebookId": {notebookID},
		"title":      {"第一篇"},
		"content":    {"# hello 世界"},
		"tags":       {"go,笔记"},
		"isNew":      {"true"},
		"isMarkdown": {"true"},
	}, &doc)
	if !doc.Writable || doc.Note.NoteId != noteID || doc.Note.UserId != userID || len(doc.Note.Tags) != 2 {
		t.Fatalf("save document mismatch: %+v", doc)
	}
	if !doc.Note.IsMarkdown {
		t.Fatal("isMarkdown lost")
	}

	var list []map[string]any
	e.postJSON(t, "/web/notes", url.Values{"notebookId": {notebookID}, "page": {"1"}}, &list)
	if len(list) != 1 || list[0]["NoteId"] != noteID || list[0]["Title"] != "第一篇" {
		t.Fatalf("notes list mismatch: %v", list)
	}

	var updated struct {
		Note struct{ Usn int }
	}
	e.postJSON(t, "/web/save", url.Values{
		"noteId":  {noteID},
		"title":   {"第一篇改"},
		"content": {"# hello2"},
		"tags":    {"go"},
		"usn":     {"0"},
	}, &updated)

	var histories []map[string]any
	e.postJSON(t, "/noteContentHistory/listHistories", url.Values{"noteId": {noteID}}, &histories)
	if len(histories) != 1 || histories[0]["Content"] != "# hello 世界" {
		t.Fatalf("history mismatch: %v", histories)
	}

	_, body = e.post(t, "/web/notes", url.Values{"key": {"hello2"}, "page": {"1"}})
	if !strings.Contains(string(body), "第一篇改") {
		t.Fatalf("search failed: %s", body)
	}

	e.post(t, "/note/deleteNote", url.Values{"noteIds[0]": {noteID}})
	var trash []map[string]any
	e.postJSON(t, "/web/notes", url.Values{"trash": {"true"}, "page": {"1"}}, &trash)
	if len(trash) != 1 {
		t.Fatalf("trash view mismatch: %v", trash)
	}

	e.postJSON(t, "/web/restore", url.Values{"noteId": {noteID}}, &loginResp)
	if loginResp["Ok"] != true {
		t.Fatalf("restore failed: %v", loginResp)
	}

	e.post(t, "/note/deleteTrash", url.Values{"noteId": {noteID}})
	e.postJSON(t, "/web/notes", url.Values{"trash": {"true"}, "page": {"1"}}, &trash)
	if len(trash) != 0 {
		t.Fatalf("note still visible after permanent delete: %v", trash)
	}
}

func TestNotebookOperations(t *testing.T) {
	e := newTestEnv(t)
	_, notebookID := e.login(t)

	noteID := utils.ObjectId()
	e.post(t, "/web/save", url.Values{
		"noteId": {noteID}, "notebookId": {notebookID}, "title": {"keep"}, "content": {"x"}, "isNew": {"true"},
	})

	var resp map[string]any
	childID := utils.ObjectId()
	e.postJSON(t, "/notebook/addNotebook", url.Values{
		"notebookId":       {childID},
		"title":            {"子笔记本"},
		"parentNotebookId": {notebookID},
	}, &resp)
	if resp["Ok"] != true {
		t.Fatalf("addNotebook failed: %v", resp)
	}

	_, body := e.get(t, "/web/bootstrap")
	if !strings.Contains(string(body), "子笔记本") {
		t.Fatalf("child notebook missing in tree: %s", body)
	}
	if !strings.Contains(string(body), `"Subs"`) {
		t.Fatalf("tree nesting missing: %s", body)
	}

	e.postJSON(t, "/notebook/updateNotebookTitle", url.Values{"notebookId": {childID}, "title": {"改名"}}, &resp)
	e.postJSON(t, "/notebook/deleteNotebook", url.Values{"notebookId": {notebookID}}, &resp)
	if resp["Ok"] != false {
		t.Fatalf("expected delete of non-empty notebook to fail: %v", resp)
	}
	e.postJSON(t, "/notebook/deleteNotebook", url.Values{"notebookId": {childID}}, &resp)
	if resp["Ok"] != true {
		t.Fatalf("deleteNotebook failed: %v", resp)
	}
}

func TestLegacyImageURLRewrite(t *testing.T) {
	e := newTestEnv(t)
	_, notebookID := e.login(t)
	noteID := utils.ObjectId()
	e.post(t, "/web/save", url.Values{
		"noteId":     {noteID},
		"notebookId": {notebookID},
		"title":      {"img"},
		"content":    {`<img src="leanote://file/getImage?fileId=50f1e5f3b3b19d1f13000001">`},
		"isNew":      {"true"},
	})
	var doc struct {
		Content string
	}
	e.postJSON(t, "/web/document", url.Values{"noteId": {noteID}}, &doc)
	if !strings.Contains(doc.Content, "/api/file/getImage?fileId=50f1e5f3b3b19d1f13000001") {
		t.Fatalf("legacy URL not rewritten: %s", doc.Content)
	}
}

func TestImageUploadAndServe(t *testing.T) {
	e := newTestEnv(t)
	_, notebookID := e.login(t)
	noteID := utils.ObjectId()
	e.post(t, "/web/save", url.Values{
		"noteId": {noteID}, "notebookId": {notebookID}, "title": {"pic"}, "content": {"x"}, "isNew": {"true"},
	})

	payload := []byte("fake-png-bytes")
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("noteId", noteID)
	part, _ := writer.CreateFormFile("file", "shot.png")
	part.Write(payload)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/file/pasteImage", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)

	var upload struct {
		Ok bool
		Id string
	}
	json.Unmarshal(rec.Body.Bytes(), &upload)
	if !upload.Ok || upload.Id == "" {
		t.Fatalf("pasteImage failed: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/file/getImage?fileId="+upload.Id, nil)
	rec = httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatalf("getImage mismatch: code=%d", rec.Code)
	}
}

func TestAttachmentRoundtrip(t *testing.T) {
	e := newTestEnv(t)
	_, notebookID := e.login(t)
	noteID := utils.ObjectId()
	e.post(t, "/web/save", url.Values{
		"noteId": {noteID}, "notebookId": {notebookID}, "title": {"doc"}, "content": {"x"}, "isNew": {"true"},
	})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("noteId", noteID)
	part, _ := writer.CreateFormFile("file", "报告.txt")
	part.Write([]byte("附件内容"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/attach/uploadAttach", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)

	var upload struct {
		Ok bool
		Id string
	}
	json.Unmarshal(rec.Body.Bytes(), &upload)
	if !upload.Ok || upload.Id == "" {
		t.Fatalf("uploadAttach failed: %s", rec.Body.String())
	}

	var listResp struct {
		Ok   bool
		List []map[string]any
	}
	e.postJSON(t, "/attach/getAttachs", url.Values{"noteId": {noteID}}, &listResp)
	if !listResp.Ok || len(listResp.List) != 1 || listResp.List[0]["Title"] != "报告.txt" || listResp.List[0]["Size"] == int64(0) {
		t.Fatalf("getAttachs mismatch: %v", listResp.List)
	}
	attachID, _ := listResp.List[0]["AttachId"].(string)

	dl := httptest.NewRequest(http.MethodGet, "/attach/download?attachId="+attachID, nil)
	rec = httptest.NewRecorder()
	e.handler.ServeHTTP(rec, dl)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("download failed: %d %s", rec.Code, rec.Header().Get("Content-Disposition"))
	}

	e.postJSON(t, "/attach/deleteAttach", url.Values{"attachId": {attachID}}, &listResp)
	if listResp.Ok != true {
		t.Fatalf("deleteAttach failed: %v", listResp)
	}
	e.postJSON(t, "/attach/getAttachs", url.Values{"noteId": {noteID}}, &listResp)
	if len(listResp.List) != 0 {
		t.Fatalf("attach still listed: %v", listResp.List)
	}
}

func TestMoveCopyAndPagination(t *testing.T) {
	e := newTestEnv(t)
	_, notebookID := e.login(t)
	otherID := utils.ObjectId()
	now := time.Now()
	e.db.InsertNotebook(&models.Notebook{ID: utils.ObjectId(), NotebookID: otherID, Title: "B", UserID: e.db.GetCurrentUserID(), CreatedTime: &now, UpdatedTime: &now})

	var ids []string
	for i := 0; i < 150; i++ {
		id := utils.ObjectId()
		ids = append(ids, id)
		e.post(t, "/web/save", url.Values{
			"noteId": {id}, "notebookId": {notebookID}, "title": {"n" + strconv.Itoa(i)}, "content": {"c"}, "isNew": {"true"},
		})
	}

	var page1 []map[string]any
	e.postJSON(t, "/web/notes", url.Values{"notebookId": {notebookID}, "page": {"1"}}, &page1)
	if len(page1) != 100 {
		t.Fatalf("page1 size = %d", len(page1))
	}
	var page2 []map[string]any
	e.postJSON(t, "/web/notes", url.Values{"notebookId": {notebookID}, "page": {"2"}}, &page2)
	if len(page2) != 50 {
		t.Fatalf("page2 size = %d", len(page2))
	}

	target := ids[0]
	e.post(t, "/note/moveNote", url.Values{"noteIds[0]": {target}, "notebookId": {otherID}})
	var otherNotes []map[string]any
	e.postJSON(t, "/web/notes", url.Values{"notebookId": {otherID}, "page": {"1"}}, &otherNotes)
	if len(otherNotes) != 1 {
		t.Fatalf("move failed: %v", otherNotes)
	}
	e.postJSON(t, "/web/notes", url.Values{"notebookId": {notebookID}, "page": {"2"}}, &page2)
	if len(page2) != 49 {
		t.Fatalf("after move page2=%d, want 49", len(page2))
	}

	e.post(t, "/note/copyNote", url.Values{"noteIds[0]": {target}, "notebookId": {notebookID}})
	e.postJSON(t, "/web/notes", url.Values{"notebookId": {notebookID}, "page": {"2"}}, &page2)
	if len(page2) != 50 {
		t.Fatalf("copy failed: page2=%d, want 50", len(page2))
	}
}

func TestSpaFallback(t *testing.T) {
	e := newTestEnv(t)
	code, body := e.get(t, "/note/some-deep-link")
	if code != 200 || !strings.Contains(string(body), "spa") {
		t.Fatalf("SPA fallback failed: %d %s", code, body)
	}
}

func TestLogoutRedirectsToLogin(t *testing.T) {
	e := newTestEnv(t)
	e.login(t)
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("logout status = %d", rec.Code)
	}
	_, body := e.get(t, "/web/bootstrap")
	if !strings.Contains(string(body), `"User":null`) {
		t.Fatalf("user still active after logout: %s", body)
	}
}

func TestDesktopLogoutReturnsJSONAndClearsSession(t *testing.T) {
	e := newTestEnv(t)
	e.login(t)
	code, body := e.post(t, "/web/logout", url.Values{})
	if code != http.StatusOK || !strings.Contains(string(body), `"Ok":true`) {
		t.Fatalf("desktop logout response = %d %s", code, body)
	}
	if active, _ := e.db.GetActiveUser(); active != nil {
		t.Fatalf("user still active after desktop logout: %+v", active)
	}
}

func TestDesktopLogoutWithoutPendingChangesDoesNotContactServer(t *testing.T) {
	e := newTestEnv(t)
	userID := utils.ObjectId()
	user := &models.User{ID: userID, Username: "remote", Host: "https://offline.example", Token: "token", IsActive: true}
	if err := e.db.InsertUser(user); err != nil {
		t.Fatal(err)
	}
	e.db.SetCurrentUser(userID)
	proxy := NewServerProxy(e.db, e.handler.Files)
	calls := 0
	proxy.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("offline")
	})
	e.handler.Proxy = proxy

	_, body := e.post(t, "/web/logout", url.Values{})
	if calls != 0 || !strings.Contains(string(body), `"Ok":true`) {
		t.Fatalf("logout contacted server or failed: calls=%d body=%s", calls, body)
	}
}

func TestOfflineRemoteAccountBootstrapUsesCache(t *testing.T) {
	e := newTestEnv(t)
	userID := utils.ObjectId()
	user := &models.User{ID: userID, Username: "remote", Email: "remote@example.test", Host: "https://offline.example", Token: "token", IsActive: true}
	if err := e.db.InsertUser(user); err != nil {
		t.Fatal(err)
	}
	e.db.SetCurrentUser(userID)
	e.db.SetConfig("host", user.Host)
	e.db.SetConfig("proxy:email", user.Email)
	e.db.SetConfig("proxy:pwd", "secret")
	e.db.SetConfig(adminCacheKey(user.Host, userID), "true")
	proxy := NewServerProxy(e.db, e.handler.Files)
	calls := 0
	proxy.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("offline")
	})
	e.handler.Proxy = proxy

	_, body := e.get(t, "/web/bootstrap")
	if calls != 0 || !strings.Contains(string(body), `"IsAdmin":true`) {
		t.Fatalf("bootstrap contacted server or lost cached admin: calls=%d body=%s", calls, body)
	}
	_, body = e.post(t, "/web/groups", url.Values{})
	if calls != 1 || !strings.Contains(string(body), `"Msg":"offline"`) {
		t.Fatalf("offline account request mismatch: calls=%d body=%s", calls, body)
	}
}

func TestRemoteLoginDoesNotUseMatchingLocalAccount(t *testing.T) {
	e := newTestEnv(t)
	userID := utils.ObjectId()
	hashed := utils.MD5WithSalt("secret", userID)
	if err := e.db.InsertUser(&models.User{ID: userID, Username: "same", Email: "same@example.test", Pwd: hashed, IsLocal: true}); err != nil {
		t.Fatal(err)
	}
	proxy := NewServerProxy(e.db, e.handler.Files)
	proxy.SetHost("http://127.0.0.1:1")
	e.handler.Proxy = proxy

	var result struct {
		Ok  bool
		Msg string
	}
	e.postJSON(t, "/doLogin", url.Values{"email": {"same@example.test"}, "pwd": {"secret"}}, &result)
	if result.Ok || result.Msg != "offline" {
		t.Fatalf("remote login unexpectedly used local account: %+v", result)
	}
	if active, _ := e.db.GetActiveUser(); active != nil {
		t.Fatalf("local account became active after failed remote login: %+v", active)
	}
}

func TestLogoutSyncFailureKeepsSession(t *testing.T) {
	e := newTestEnv(t)
	userID := utils.ObjectId()
	if err := e.db.InsertUser(&models.User{ID: userID, Username: "remote", Host: "https://notes.example", Token: "token", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	e.handler.OnLogout = func() error { return fmt.Errorf("sync unavailable") }
	code, body := e.get(t, "/logout")
	if code != http.StatusOK || !strings.Contains(string(body), "syncFailed") {
		t.Fatalf("logout did not report sync failure: %d %s", code, body)
	}
	active, _ := e.db.GetActiveUser()
	if active == nil || active.Token != "token" {
		t.Fatalf("session was cleared after failed sync: %+v", active)
	}
	_, body = e.post(t, "/web/logout", url.Values{})
	if !strings.Contains(string(body), `"Msg":"syncFailed"`) {
		t.Fatalf("desktop logout did not report sync failure: %s", body)
	}
}
