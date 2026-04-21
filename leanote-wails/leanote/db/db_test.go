package db

import (
	"testing"

	"leanote/models"
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
