package sync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gemsnote/gemsnote/api"
	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
)

func TestFullSyncRepushesMissingDesktopNote(t *testing.T) {
	addCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/user/getSyncState":
			w.Write([]byte(`{"LastSyncUsn":10,"LastSyncTime":0}`))
		case "/api2/notebook/getSyncNotebooks":
			json.NewEncoder(w).Encode([]map[string]any{{"NotebookId": "remote-book", "UserId": "user1", "Title": "Book", "Usn": 1}})
		case "/api2/note/getSyncNotes", "/api2/tag/getSyncTags":
			w.Write([]byte(`[]`))
		case "/api2/note/addNote":
			addCalls++
			if err := r.ParseForm(); err != nil || r.Form.Get("NotebookId") != "remote-book" {
				t.Errorf("wrong notebook sent to server: %v %v", r.Form, err)
			}
			w.Write([]byte(`{"NoteId":"remote-new","NotebookId":"remote-book","Usn":11,"Title":"Offline"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester", Host: server.URL, Token: "token", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser("user1")
	if err := database.InsertNotebook(&models.Notebook{ID: "b", NotebookID: "local-book", ServerNotebookID: "remote-book", UserID: "user1", Usn: 1}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNote(&models.Note{ID: "n", NoteID: "local-note", ServerNoteID: "gone-note", NotebookID: "local-book", UserID: "user1", Title: "Offline", Content: "body", Usn: 9}); err != nil {
		t.Fatal(err)
	}
	if err := NewSyncService(database, api.NewClient()).ForceFullSync(); err != nil {
		t.Fatal(err)
	}
	note, err := database.GetNote("local-note")
	if err != nil || note.ServerNoteID != "remote-new" || note.IsDirty || addCalls != 1 {
		t.Fatalf("note not repushed: note=%+v calls=%d err=%v", note, addCalls, err)
	}
}

func TestSameUSNNoteMoveUpdatesLocalNotebook(t *testing.T) {
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}
	for _, book := range []*models.Notebook{
		{ID: "a", NotebookID: "local-a", ServerNotebookID: "remote-a", UserID: "user1"},
		{ID: "b", NotebookID: "local-b", ServerNotebookID: "remote-b", UserID: "user1"},
	} {
		if err := database.InsertNotebook(book); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.InsertNote(&models.Note{ID: "n", NoteID: "local-note", ServerNoteID: "remote-note", NotebookID: "local-a", UserID: "user1", Content: "cached", Usn: 12}); err != nil {
		t.Fatal(err)
	}
	svc := NewSyncService(database, api.NewClient())
	if err := svc.processNoteSync(&models.Note{NoteID: "remote-note", NotebookID: "remote-b", Usn: 12}, models.NewSyncInfo()); err != nil {
		t.Fatal(err)
	}
	note, err := database.GetNote("local-note")
	if err != nil || note.NotebookID != "local-b" {
		t.Fatalf("notebook move not applied: note=%+v err=%v", note, err)
	}
}

func TestSameUSNRootNotebookClearsStaleParent(t *testing.T) {
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNotebook(&models.Notebook{ID: "n", NotebookID: "local-root", ServerNotebookID: "remote-root", ParentNotebookID: "local-root", UserID: "user1", Title: "Root", Usn: 7}); err != nil {
		t.Fatal(err)
	}
	if err := NewSyncService(database, api.NewClient()).processNotebookSync(&models.Notebook{NotebookID: "remote-root", ParentNotebookID: "", UserID: "user1", Title: "Root", Usn: 7}, models.NewSyncInfo()); err != nil {
		t.Fatal(err)
	}
	nb, err := database.GetNotebook("local-root")
	if err != nil || nb == nil || nb.ParentNotebookID != "" {
		t.Fatalf("stale root parent retained: %+v %v", nb, err)
	}
}

func TestFullSyncMergesChangedServerDatabase(t *testing.T) {
	addedNotebook, addedNote := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/user/getSyncState":
			w.Write([]byte(`{"LastSyncUsn":3,"LastSyncTime":0}`))
		case "/api2/notebook/getSyncNotebooks":
			w.Write([]byte(`[{"NotebookId":"remote-book","UserId":"user1","Title":"Remote","Usn":1}]`))
		case "/api2/note/getSyncNotes":
			w.Write([]byte(`[{"NoteId":"remote-only","NotebookId":"remote-book","UserId":"user1","Title":"Remote note","Usn":2},{"NoteId":"old-note","IsDeleted":true,"Usn":3}]`))
		case "/api2/note/getNoteContent":
			w.Write([]byte(`{"Content":"remote body"}`))
		case "/api2/tag/getSyncTags":
			w.Write([]byte(`[]`))
		case "/api2/client/notebook/add":
			addedNotebook++
			w.Write([]byte(`{"NotebookId":"new-book","Title":"Offline","Usn":4}`))
		case "/api2/note/addNote":
			addedNote++
			if err := r.ParseForm(); err != nil || r.Form.Get("NotebookId") != "new-book" {
				t.Errorf("note sent to wrong notebook: %v %v", r.Form, err)
			}
			json.NewEncoder(w).Encode(map[string]any{"NoteId": fmt.Sprintf("new-note-%d", addedNote), "NotebookId": "new-book", "Usn": 4 + addedNote})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester", Host: server.URL, Token: "token", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser("user1")
	if err := database.InsertNotebook(&models.Notebook{ID: "b", NotebookID: "old-book", ServerNotebookID: "old-book", UserID: "user1", Title: "Offline"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNote(&models.Note{ID: "old", NoteID: "old-note", ServerNoteID: "old-note", NotebookID: "old-book", UserID: "user1", Title: "Old cache"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNote(&models.Note{ID: "local", NoteID: "local-note", ServerNoteID: "missing-note", NotebookID: "old-book", UserID: "user1", Title: "Local work", Content: "body"}); err != nil {
		t.Fatal(err)
	}
	if err := NewSyncService(database, api.NewClient()).ForceFullSync(); err != nil {
		t.Fatal(err)
	}
	if addedNotebook != 1 || addedNote != 2 {
		t.Fatalf("uploads: notebooks=%d notes=%d", addedNotebook, addedNote)
	}
	oldNote, err := database.GetNote("old-note")
	if err != nil || oldNote == nil || oldNote.ServerNoteID == "old-note" || oldNote.IsDirty || oldNote.LocalIsDelete {
		t.Fatalf("old cache not merged: %+v %v", oldNote, err)
	}
	localNote, err := database.GetNote("local-note")
	if err != nil || localNote == nil || localNote.ServerNoteID == "missing-note" || localNote.IsDirty || localNote.LocalIsDelete {
		t.Fatalf("local note not uploaded: %+v %v", localNote, err)
	}
	remoteNote, err := database.GetNoteByServerID("remote-only")
	if err != nil || remoteNote == nil || remoteNote.Content != "remote body" {
		t.Fatalf("remote-only note not downloaded: %+v %v", remoteNote, err)
	}
}

func TestIncrementalSyncDetectsServerRollback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/user/getSyncState":
			w.Write([]byte(`{"LastSyncUsn":3,"LastSyncTime":0}`))
		case "/api2/notebook/getSyncNotebooks", "/api2/note/getSyncNotes", "/api2/tag/getSyncTags":
			w.Write([]byte(`[]`))
		case "/api2/client/notebook/add":
			w.Write([]byte(`{"NotebookId":"new-book","Usn":4}`))
		case "/api2/note/addNote":
			w.Write([]byte(`{"NoteId":"new-note","NotebookId":"new-book","Usn":5}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester", Host: server.URL, Token: "token", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser("user1")
	if err := database.InsertNotebook(&models.Notebook{ID: "b", NotebookID: "old-book", ServerNotebookID: "old-book", UserID: "user1"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNote(&models.Note{ID: "n", NoteID: "old-note", ServerNoteID: "old-note", NotebookID: "old-book", UserID: "user1"}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateUserSyncState("user1", map[string]int64{"last_sync_usn": 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSyncService(database, api.NewClient()).IncrSync(); err != nil {
		t.Fatal(err)
	}
	note, err := database.GetNote("old-note")
	if err != nil || note == nil || note.ServerNoteID != "new-note" || note.IsDirty || note.LocalIsDelete {
		t.Fatalf("rollback did not merge old cache: %+v %v", note, err)
	}
}

func TestFullSyncUploadsMissingNotebookTreeParentFirst(t *testing.T) {
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/user/getSyncState":
			w.Write([]byte(`{"LastSyncUsn":1,"LastSyncTime":0}`))
		case "/api2/notebook/getSyncNotebooks", "/api2/note/getSyncNotes", "/api2/tag/getSyncTags":
			w.Write([]byte(`[]`))
		case "/api2/client/notebook/add":
			_ = r.ParseForm()
			order = append(order, r.Form.Get("title"))
			if r.Form.Get("title") == "Parent" {
				w.Write([]byte(`{"NotebookId":"new-parent","Title":"Parent","Usn":2}`))
			} else {
				if r.Form.Get("parentNotebookId") != "new-parent" {
					t.Errorf("child parent ID = %q", r.Form.Get("parentNotebookId"))
				}
				w.Write([]byte(`{"NotebookId":"new-child","ParentNotebookId":"new-parent","Title":"Child","Usn":3}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester", Host: server.URL, Token: "token", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser("user1")
	for _, nb := range []*models.Notebook{
		{ID: "c", NotebookID: "old-child", ServerNotebookID: "old-child", ParentNotebookID: "old-parent", UserID: "user1", Title: "Child"},
		{ID: "p", NotebookID: "old-parent", ServerNotebookID: "old-parent", UserID: "user1", Title: "Parent"},
	} {
		if err := database.InsertNotebook(nb); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewSyncService(database, api.NewClient()).ForceFullSync(); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "Parent" || order[1] != "Child" {
		t.Fatalf("upload order: %v", order)
	}
	child, err := database.GetNotebook("old-child")
	if err != nil || child == nil || child.ServerNotebookID != "new-child" || child.ParentNotebookID != "old-parent" || child.IsDirty {
		t.Fatalf("child mapping: %+v %v", child, err)
	}
}
