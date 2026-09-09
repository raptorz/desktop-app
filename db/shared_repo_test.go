package db

import (
	"os"
	"path/filepath"
	"testing"

	"pearlnote/models"
)

func TestSharedSnapshotAccountIsolationAndRevocation(t *testing.T) {
	d, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a := SharedAccountID("https://one.example/", "507f1f77bcf86cd799439011")
	b := SharedAccountID("https://two.example", "507f1f77bcf86cd799439011")
	if a == b || len(a) != 24 {
		t.Fatalf("invalid account ids %q %q", a, b)
	}
	items := []models.SharedSnapshotItem{
		{Kind: "notebook", Notebook: &models.SharedNotebook{NotebookID: "507f1f77bcf86cd799439012", OwnerUserID: "507f1f77bcf86cd799439013", Title: "Shared"}},
		{Kind: "note", Note: &models.SharedNote{NoteID: "507f1f77bcf86cd799439014", NotebookID: "507f1f77bcf86cd799439012", OwnerUserID: "507f1f77bcf86cd799439013", Title: "N", TargetContentVersion: "v1"}},
	}
	if err := d.PublishSharedSnapshot(a, items, len(items)); err != nil {
		t.Fatal(err)
	}
	if note, err := d.GetSharedNote(a, items[1].Note.NoteID); err != nil || note == nil {
		t.Fatalf("shared note missing: %v", err)
	}
	if note, _ := d.GetSharedNote(b, items[1].Note.NoteID); note != nil {
		t.Fatal("shared note leaked across accounts")
	}
	if err := d.PublishSharedSnapshot(a, nil, 0); err != nil {
		t.Fatal(err)
	}
	if note, _ := d.GetSharedNote(a, items[1].Note.NoteID); note != nil {
		t.Fatal("revoked note remained readable")
	}
}

func TestIncompleteSharedSnapshotDoesNotPublish(t *testing.T) {
	d, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a := SharedAccountID("https://one.example", "u")
	item := models.SharedSnapshotItem{Kind: "note", Note: &models.SharedNote{NoteID: "507f1f77bcf86cd799439014", OwnerUserID: "owner", TargetContentVersion: "v1"}}
	if err := d.PublishSharedSnapshot(a, []models.SharedSnapshotItem{item}, 2); err == nil {
		t.Fatal("expected count mismatch")
	}
	if note, _ := d.GetSharedNote(a, item.Note.NoteID); note != nil {
		t.Fatal("partial snapshot was published")
	}
}

func TestSingleSharedNoteCreatesVirtualOwnerSection(t *testing.T) {
	d, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	accountID := SharedAccountID("https://one.example", "507f1f77bcf86cd799439011")
	ownerID := "507f1f77bcf86cd799439013"
	item := models.SharedSnapshotItem{Kind: "note", Note: &models.SharedNote{
		NoteID: "507f1f77bcf86cd799439014", OwnerUserID: ownerID, TargetContentVersion: "v1",
	}}
	if err := d.PublishSharedSnapshot(accountID, []models.SharedSnapshotItem{item}, 1); err != nil {
		t.Fatal(err)
	}
	sections, err := d.SharedNotebooks(accountID)
	if err != nil {
		t.Fatal(err)
	}
	books, ok := sections[ownerID].([]map[string]any)
	if !ok || len(books) != 1 || books[0]["IsDefault"] != true {
		t.Fatalf("single-note virtual section missing: %#v", sections)
	}
}

func TestSharedFileCacheLifecycle(t *testing.T) {
	d, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a := SharedAccountID("https://one.example", "507f1f77bcf86cd799439011")
	items := []models.SharedSnapshotItem{
		{Kind: "note", Note: &models.SharedNote{NoteID: "507f1f77bcf86cd799439014", OwnerUserID: "507f1f77bcf86cd799439013", TargetContentVersion: "v1"}},
		{Kind: "file", File: &models.SharedSnapshotFile{NoteID: "507f1f77bcf86cd799439014", FileID: "507f1f77bcf86cd799439015", Kind: "image", Title: "pic.png", Size: 3, Version: "imgv1"}},
	}
	if err := d.PublishSharedSnapshot(a, items, len(items)); err != nil {
		t.Fatal(err)
	}

	jobs, err := d.PendingSharedFileJobs(a, []string{"image"}, 10)
	if err != nil || len(jobs) != 1 || jobs[0].FileID != "507f1f77bcf86cd799439015" {
		t.Fatalf("file job missing: %v %v", jobs, err)
	}
	cacheDir := t.TempDir()
	local := filepath.Join(cacheDir, "pic.png")
	if err := os.WriteFile(local, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := d.CompleteSharedFile(a, jobs[0], local); err != nil {
		t.Fatal(err)
	}
	path, title, kind, err := d.GetSharedFilePath(a, "507f1f77bcf86cd799439015")
	if err != nil || path != local || title != "pic.png" || kind != "image" {
		t.Fatalf("shared file path mismatch: %v %v %v %v", path, title, kind, err)
	}

	if err := d.PublishSharedSnapshot(a, nil, 0); err != nil {
		t.Fatal(err)
	}
	if path, _, _, _ := d.GetSharedFilePath(a, "507f1f77bcf86cd799439015"); path != "" {
		t.Fatal("revoked file remained readable")
	}
	if !d.IsSharedFile(a, "507f1f77bcf86cd799439015") {
		t.Fatal("shared file row lost after revocation")
	}
}

func TestSharedAttachmentIsDownloadedOnlyAfterQueue(t *testing.T) {
	d, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	accountID := SharedAccountID("https://one.example", "507f1f77bcf86cd799439011")
	noteID := "507f1f77bcf86cd799439014"
	fileID := "507f1f77bcf86cd799439016"
	items := []models.SharedSnapshotItem{
		{Kind: "note", Note: &models.SharedNote{NoteID: noteID, OwnerUserID: "507f1f77bcf86cd799439013", TargetContentVersion: "v1"}},
		{Kind: "file", File: &models.SharedSnapshotFile{NoteID: noteID, FileID: fileID, Kind: "attachment", Title: "report.pdf", Size: 10, Version: "fv1"}},
	}
	if err := d.PublishSharedSnapshot(accountID, items, len(items)); err != nil {
		t.Fatal(err)
	}
	if jobs, err := d.PendingSharedFileJobs(accountID, []string{"attachment"}, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("attachment downloaded before queue: jobs=%v err=%v", jobs, err)
	}
	attachments, err := d.ListSharedAttachmentsForNote(accountID, noteID)
	if err != nil || len(attachments) != 1 || attachments[0].CacheState != "available" {
		t.Fatalf("attachment availability mismatch: %+v err=%v", attachments, err)
	}
	if n, err := d.QueueSharedAttachment(accountID, fileID); err != nil || n != 1 {
		t.Fatalf("queue attachment: affected=%d err=%v", n, err)
	}
	if jobs, err := d.PendingSharedFileJobs(accountID, []string{"attachment"}, 10); err != nil || len(jobs) != 1 || jobs[0].FileID != fileID {
		t.Fatalf("queued attachment missing: jobs=%v err=%v", jobs, err)
	}
}
