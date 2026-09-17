package db

import (
	"testing"

	"github.com/gemsnote/gemsnote/models"
)

func TestNewInMemory(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create in-memory database: %v", err)
	}
	defer database.Close()
}

func TestInsertAndGetUser(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{
		ID:       "user1",
		Username: "testuser",
		Email:    "test@example.com",
		Token:    "token123",
		IsActive: true,
	}

	err = database.InsertUser(user)
	if err != nil {
		t.Fatalf("Failed to insert user: %v", err)
	}

	got, err := database.GetActiveUser()
	if err != nil {
		t.Fatalf("Failed to get active user: %v", err)
	}

	if got == nil {
		t.Fatal("Expected user, got nil")
	}

	if got.ID != "user1" {
		t.Errorf("Expected ID 'user1', got '%s'", got.ID)
	}
	if got.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got '%s'", got.Username)
	}
}

func TestUpdateLastSyncUsn(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{
		ID:          "user1",
		Username:    "testuser",
		IsActive:    true,
		LastSyncUsn: 10,
		NotebookUsn: 5,
		NoteUsn:     8,
		TagUsn:      3,
	}
	database.InsertUser(user)

	err = database.UpdateLastSyncUsn("user1", 20)
	if err != nil {
		t.Fatalf("Failed to update sync usn: %v", err)
	}

	lastUsn, _, _, _, err := database.GetAllLastSyncState("user1")
	if err != nil {
		t.Fatalf("Failed to get sync state: %v", err)
	}

	if lastUsn != 20 {
		t.Errorf("Expected lastUsn 20, got %d", lastUsn)
	}
}

func TestInsertAndGetNotebook(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb := &models.Notebook{
		ID:         "nb1",
		NotebookID: "nbid1",
		UserID:     "user1",
		Title:      "Test Notebook",
		Seq:        0,
		IsDirty:    true,
		LocalIsNew: true,
	}

	err = database.InsertNotebook(nb)
	if err != nil {
		t.Fatalf("Failed to insert notebook: %v", err)
	}

	got, err := database.GetNotebook("nbid1")
	if err != nil {
		t.Fatalf("Failed to get notebook: %v", err)
	}

	if got == nil {
		t.Fatal("Expected notebook, got nil")
	}

	if got.Title != "Test Notebook" {
		t.Errorf("Expected Title 'Test Notebook', got '%s'", got.Title)
	}
}

func TestNotebookByServerID(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb := &models.Notebook{
		ID:               "nb1",
		NotebookID:       "nbid1",
		ServerNotebookID: "server_nb1",
		UserID:           "user1",
		Title:            "Test",
	}
	database.InsertNotebook(nb)

	got, err := database.GetNotebookByServerID("server_nb1")
	if err != nil {
		t.Fatalf("Failed to get notebook by server ID: %v", err)
	}

	if got == nil {
		t.Fatal("Expected notebook, got nil")
	}
	if got.NotebookID != "nbid1" {
		t.Errorf("Expected NotebookID 'nbid1', got '%s'", got.NotebookID)
	}
}

func TestDeleteNotebook(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb := &models.Notebook{
		ID:         "nb1",
		NotebookID: "nbid1",
		UserID:     "user1",
		Title:      "Test",
	}
	database.InsertNotebook(nb)

	err = database.DeleteNotebook("nbid1")
	if err != nil {
		t.Fatalf("Failed to delete notebook: %v", err)
	}

	got, _ := database.GetNotebook("nbid1")
	if got == nil {
		t.Fatal("Notebook should still exist (soft delete)")
	}
	if !got.LocalIsDelete {
		t.Error("Expected LocalIsDelete to be true")
	}
}

func TestGetDirtyNotebooks(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb1 := &models.Notebook{
		ID: "nb1", NotebookID: "nbid1", UserID: "user1", Title: "Dirty", IsDirty: true,
	}
	nb2 := &models.Notebook{
		ID: "nb2", NotebookID: "nbid2", UserID: "user1", Title: "Clean", IsDirty: false,
	}

	database.InsertNotebook(nb1)
	database.InsertNotebook(nb2)

	dirty, err := database.GetDirtyNotebooks("user1")
	if err != nil {
		t.Fatalf("Failed to get dirty notebooks: %v", err)
	}

	if len(dirty) != 1 {
		t.Fatalf("Expected 1 dirty notebook, got %d", len(dirty))
	}
	if dirty[0].NotebookID != "nbid1" {
		t.Errorf("Expected dirty notebook 'nbid1', got '%s'", dirty[0].NotebookID)
	}
}

func TestMapNotebooks(t *testing.T) {
	notebooks := []*models.Notebook{
		{NotebookID: "1", Title: "Root1", Seq: 0, ParentNotebookID: ""},
		{NotebookID: "2", Title: "Child1", Seq: 0, ParentNotebookID: "1"},
		{NotebookID: "3", Title: "Root2", Seq: 1, ParentNotebookID: ""},
	}

	database, _ := NewInMemory()
	defer database.Close()

	roots := database.MapNotebooks(notebooks)

	if len(roots) != 2 {
		t.Fatalf("Expected 2 root notebooks, got %d", len(roots))
	}
	if roots[0].NotebookID != "1" {
		t.Errorf("Expected first root '1', got '%s'", roots[0].NotebookID)
	}
	if len(roots[0].Subs) != 1 {
		t.Errorf("Expected 1 child, got %d", len(roots[0].Subs))
	}
}

func TestInsertAndGetNote(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb := &models.Notebook{ID: "nb1", NotebookID: "nbid1", UserID: "user1", Title: "Test"}
	database.InsertNotebook(nb)

	note := &models.Note{
		ID:         "note1",
		NoteID:     "noteid1",
		NotebookID: "nbid1",
		UserID:     "user1",
		Title:      "Test Note",
		Content:    "Test content",
		IsDirty:    true,
	}

	err = database.InsertNote(note)
	if err != nil {
		t.Fatalf("Failed to insert note: %v", err)
	}

	got, err := database.GetNote("noteid1")
	if err != nil {
		t.Fatalf("Failed to get note: %v", err)
	}

	if got == nil {
		t.Fatal("Expected note, got nil")
	}
	if got.Title != "Test Note" {
		t.Errorf("Expected Title 'Test Note', got '%s'", got.Title)
	}
}

func TestGetNotes(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb := &models.Notebook{ID: "nb1", NotebookID: "nbid1", UserID: "user1", Title: "Test"}
	database.InsertNotebook(nb)

	note1 := &models.Note{ID: "n1", NoteID: "nid1", NotebookID: "nbid1", UserID: "user1", Title: "Note 1"}
	note2 := &models.Note{ID: "n2", NoteID: "nid2", NotebookID: "nbid1", UserID: "user1", Title: "Note 2"}

	database.InsertNote(note1)
	database.InsertNote(note2)

	notes, err := database.GetNotes("nbid1")
	if err != nil {
		t.Fatalf("Failed to get notes: %v", err)
	}

	if len(notes) != 2 {
		t.Errorf("Expected 2 notes, got %d", len(notes))
	}
}

func TestUpdateNoteForcePreservesCachedContent(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNotebook(&models.Notebook{ID: "nb1", NotebookID: "nbid1", UserID: "user1", Title: "Notebook"}); err != nil {
		t.Fatal(err)
	}
	note := &models.Note{ID: "n1", NoteID: "nid1", ServerNoteID: "nid1", NotebookID: "nbid1", UserID: "user1", Title: "Before", Content: "cached body", IsStar: false}
	if err := database.InsertNote(note); err != nil {
		t.Fatal(err)
	}
	remote := &models.Note{NoteID: "nid1", Title: "After", IsStar: true, Usn: 2}
	if err := database.UpdateNoteForce(remote, true); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetNote("nid1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "cached body" || !got.IsStar || got.Title != "After" {
		t.Fatalf("metadata sync damaged cached note: %+v", got)
	}
}

func TestRequeueMissingDesktopNotes(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNotebook(&models.Notebook{ID: "nb1", NotebookID: "book1", UserID: "user1"}); err != nil {
		t.Fatal(err)
	}
	for _, note := range []*models.Note{
		{ID: "a", NoteID: "local-a", ServerNoteID: "missing-a", NotebookID: "book1", UserID: "user1"},
		{ID: "b", NoteID: "local-b", ServerNoteID: "present-b", NotebookID: "book1", UserID: "user1"},
		{ID: "c", NoteID: "server-c", ServerNoteID: "server-c", NotebookID: "book1", UserID: "user1"},
	} {
		if err := database.InsertNote(note); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.RequeueMissingDesktopNotes("user1", map[string]bool{"present-b": true}); err != nil {
		t.Fatal(err)
	}
	a, _ := database.GetNote("local-a")
	b, _ := database.GetNote("local-b")
	c, _ := database.GetNote("server-c")
	if !a.IsDirty || !a.LocalIsNew || a.ServerNoteID != "" || b.IsDirty || !c.IsDirty || !c.LocalIsNew || c.ServerNoteID != "" {
		t.Fatalf("requeue mismatch: a=%+v b=%+v c=%+v", a, b, c)
	}
}

func TestReconcileFullNotebooksRequeuesDirtyAndHiddenRows(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}
	for _, nb := range []*models.Notebook{
		{ID: "a", NotebookID: "dirty-book", ServerNotebookID: "old-dirty", UserID: "user1", IsDirty: true, Title: "Edited"},
		{ID: "b", NotebookID: "hidden-book", ServerNotebookID: "old-hidden", UserID: "user1", LocalIsDelete: true, Title: "Hidden"},
	} {
		if err := database.InsertNotebook(nb); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.ReconcileFullNotebooks("user1", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dirty-book", "hidden-book"} {
		nb, err := database.GetNotebook(id)
		if err != nil || nb == nil || nb.ServerNotebookID != "" || !nb.IsDirty || !nb.LocalIsNew || nb.LocalIsDelete {
			t.Fatalf("notebook %s not queued for merge: %+v %v", id, nb, err)
		}
	}
}

func TestCleanupUnusedTagsRemovesEmptyTags(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []*models.Tag{{ID: "empty", Tag: "", UserID: "user1"}, {ID: "unused", Tag: "unused", UserID: "user1"}, {ID: "used", Tag: "used", UserID: "user1"}} {
		if err := database.InsertTag(tag); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.InsertNotebook(&models.Notebook{ID: "nb", NotebookID: "book", UserID: "user1"}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertNote(&models.Note{ID: "n", NoteID: "note", NotebookID: "book", UserID: "user1", Tags: []string{"used"}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CleanupUnusedTags("user1"); err != nil {
		t.Fatal(err)
	}
	tags, err := database.GetTags("user1")
	if err != nil || len(tags) != 1 || tags[0].Tag != "used" || tags[0].Count != 1 {
		t.Fatalf("tags after cleanup: %+v %v", tags, err)
	}
}

func TestMapNotebooksKeepsCyclicNotebooksVisible(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	items := []*models.Notebook{
		{NotebookID: "root", Title: "Root"},
		{NotebookID: "self", ParentNotebookID: "self", Title: "Self"},
		{NotebookID: "child", ParentNotebookID: "self", Title: "Child"},
	}
	mapped := database.MapNotebooks(items)
	seen := map[string]bool{}
	var walk func([]*models.Notebook)
	walk = func(nodes []*models.Notebook) {
		for _, n := range nodes {
			seen[n.NotebookID] = true
			walk(n.Subs)
		}
	}
	walk(mapped)
	if len(seen) != len(items) {
		t.Fatalf("cyclic notebooks disappeared: roots=%+v seen=%v", mapped, seen)
	}
}

func TestDeleteNote(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer database.Close()

	user := &models.User{ID: "user1", Username: "testuser", IsActive: true}
	database.InsertUser(user)

	nb := &models.Notebook{ID: "nb1", NotebookID: "nbid1", UserID: "user1", Title: "Test"}
	database.InsertNotebook(nb)

	note := &models.Note{ID: "n1", NoteID: "nid1", NotebookID: "nbid1", UserID: "user1", Title: "Note"}
	database.InsertNote(note)

	err = database.DeleteNote("nid1")
	if err != nil {
		t.Fatalf("Failed to delete note: %v", err)
	}

	got, _ := database.GetNote("nid1")
	if got == nil {
		t.Fatal("Note should still exist (soft delete)")
	}
	if !got.IsTrash {
		t.Error("Expected IsTrash to be true")
	}
}

func TestHasPendingChanges(t *testing.T) {
	database, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester"}); err != nil {
		t.Fatal(err)
	}

	pending, err := database.HasPendingChanges("user1")
	if err != nil || pending {
		t.Fatalf("empty account pending=%v err=%v", pending, err)
	}
	if err := database.InsertNotebook(&models.Notebook{ID: "nb1", NotebookID: "nb1", UserID: "user1", Title: "dirty", IsDirty: true}); err != nil {
		t.Fatal(err)
	}
	pending, err = database.HasPendingChanges("user1")
	if err != nil || !pending {
		t.Fatalf("dirty account pending=%v err=%v", pending, err)
	}
	if err := database.SetNotebookNotDirty("nb1"); err != nil {
		t.Fatal(err)
	}
	pending, err = database.HasPendingChanges("user1")
	if err != nil || pending {
		t.Fatalf("clean account pending=%v err=%v", pending, err)
	}
}
