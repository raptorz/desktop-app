package sharedsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gemsnote/gemsnote/api"
	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/service"
)

const (
	testNoteID  = "60af1f77bcf86cd799439014"
	testImageID = "60af1f77bcf86cd799439015"
	testOwner   = "60af1f77bcf86cd799439013"
	testUser    = "60af1f77bcf86cd799439011"
)

type mockServer struct {
	mu            sync.Mutex
	srv           *httptest.Server
	failPages     bool
	denyContent   bool
	emptySnapshot bool
	imageBytes    []byte
	contentHits   int
	snapshotHits  int
	deniedOnce    bool
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (m *mockServer) noteItem(content string) models.SharedSnapshotItem {
	return models.SharedSnapshotItem{Kind: "note", Note: &models.SharedNote{
		NoteID: testNoteID, OwnerUserID: testOwner, Title: "Shared note", TargetContentVersion: digest([]byte(content)),
	}}
}

func (m *mockServer) fileItem() models.SharedSnapshotItem {
	return models.SharedSnapshotItem{Kind: "file", File: &models.SharedSnapshotFile{
		NoteID: testNoteID, FileID: testImageID, Kind: "image", Title: "pic.png", Size: int64(len(m.imageBytes)), Version: digest(m.imageBytes),
	}}
}

func (m *mockServer) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	switch r.URL.Path {
	case "/api/shared/capabilities":
		json.NewEncoder(w).Encode(map[string]any{"Ok": true, "ProtocolVersion": 1, "Snapshot": true})

	case "/api/shared/snapshots":
		m.snapshotHits++
		if m.emptySnapshot {
			json.NewEncoder(w).Encode(map[string]any{"Ok": true, "SnapshotId": "snap", "Total": 0})
			return
		}
		total := 2
		if m.denyContent && m.deniedOnce {
			total = 1
		}
		json.NewEncoder(w).Encode(map[string]any{"Ok": true, "SnapshotId": "snap", "Total": total})

	case "/api/shared/snapshots/snap/items":
		if m.emptySnapshot {
			json.NewEncoder(w).Encode(map[string]any{"Ok": true, "Items": []any{}, "NextPageToken": "", "Complete": true, "Total": 0})
			return
		}
		if m.failPages {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		if r.URL.Query().Get("pageToken") == "" {
			listed := !m.denyContent || !m.deniedOnce
			total := 2
			page := []models.SharedSnapshotItem{m.noteItem("shared body")}
			if !listed {
				total = 1
				page = nil
			}
			json.NewEncoder(w).Encode(map[string]any{"Ok": true, "Items": page, "NextPageToken": "2", "Complete": false, "Total": total})
			return
		}
		page := []models.SharedSnapshotItem{m.fileItem()}
		json.NewEncoder(w).Encode(map[string]any{"Ok": true, "Items": page, "NextPageToken": "", "Complete": true, "Total": 2})

	case "/api/shared/notes/" + testNoteID + "/content":
		m.contentHits++
		defer func() { m.deniedOnce = true }()
		if m.denyContent {
			json.NewEncoder(w).Encode(map[string]any{"Ok": false, "Msg": "noPermission"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Ok": true, "NoteId": testNoteID, "Version": digest([]byte("shared body")), "Digest": digest([]byte("shared body")), "Content": "shared body"})

	case "/api/shared/notes/" + testNoteID + "/files/" + testImageID:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Gemsnote-SHA256", digest(m.imageBytes))
		w.Write(m.imageBytes)

	default:
		http.NotFound(w, r)
	}
}

func newTestService(t *testing.T, mutate func(*mockServer)) (*Service, *mockServer) {
	t.Helper()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	server := &mockServer{imageBytes: []byte("fake-png")}
	if mutate != nil {
		mutate(server)
	}
	server.srv = httptest.NewServer(http.HandlerFunc(server.handler))
	t.Cleanup(server.srv.Close)

	if err := database.InsertUser(&models.User{
		ID: testUser, Username: "tester", Email: "t@gemsnote.test", Token: "tok", Host: server.srv.URL, IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser(testUser)

	files := service.NewFileService(database)
	files.SetDataDir(t.TempDir())
	client := api.NewClient()
	svc := New(database, client, files)
	return svc, server
}

func assertReadableNote(t *testing.T, database *db.Database, host, content string) {
	t.Helper()
	accountID := db.SharedAccountID(host, testUser)
	note, err := database.GetSharedNote(accountID, testNoteID)
	if err != nil || note == nil {
		t.Fatalf("shared note missing: %v", err)
	}
	if note.Content != content {
		t.Fatalf("cached content = %q, want %q", note.Content, content)
	}
}

func TestSyncHappyPathDownloadsContentAndImage(t *testing.T) {
	svc, server := newTestService(t, nil)
	if err := svc.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	assertReadableNote(t, svc.db, server.srv.URL, "shared body")

	accountID := db.SharedAccountID(server.srv.URL, testUser)
	path, _, kind, err := svc.db.GetSharedFilePath(accountID, testImageID)
	if err != nil || path == "" || kind != "image" {
		t.Fatalf("cached image missing: path=%q kind=%q err=%v", path, kind, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "fake-png" {
		t.Fatalf("cached image bytes mismatch: %v", err)
	}
}

func TestPaginationFailureKeepsPreviousCache(t *testing.T) {
	svc, server := newTestService(t, nil)
	if err := svc.SyncOnce(); err != nil {
		t.Fatal(err)
	}

	server.mu.Lock()
	server.failPages = true
	server.mu.Unlock()
	if err := svc.SyncOnce(); err == nil {
		t.Fatal("expected pagination failure")
	}

	assertReadableNote(t, svc.db, server.srv.URL, "shared body")
}

func TestValidEmptySnapshotRevokesEverything(t *testing.T) {
	svc, server := newTestService(t, nil)
	if err := svc.SyncOnce(); err != nil {
		t.Fatal(err)
	}

	revokedCh := make(chan []string, 1)
	svc.OnRevocation = func(ids []string) { revokedCh <- ids }

	server.mu.Lock()
	server.emptySnapshot = true
	server.mu.Unlock()
	if err := svc.SyncOnce(); err != nil {
		t.Fatal(err)
	}

	select {
	case ids := <-revokedCh:
		if len(ids) != 1 || ids[0] != testNoteID {
			t.Fatalf("revocation ids mismatch: %v", ids)
		}
	default:
		t.Fatal("OnRevocation not fired")
	}
	accountID := db.SharedAccountID(server.srv.URL, testUser)
	if note, _ := svc.db.GetSharedNote(accountID, testNoteID); note != nil {
		t.Fatal("revoked note still readable")
	}
	if path, _, _, _ := svc.db.GetSharedFilePath(accountID, testImageID); path != "" {
		t.Fatal("revoked file still readable")
	}
	if !svc.db.IsSharedFile(accountID, testImageID) {
		t.Fatal("revoked file row should be retained for cleanup accounting")
	}
}

func TestNoPermissionDuringDownloadRevokesAndRebuilds(t *testing.T) {
	server := &mockServer{imageBytes: []byte("fake-png")}
	srv := httptest.NewServer(http.HandlerFunc(server.handler))
	defer srv.Close()

	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{
		ID: testUser, Username: "tester", Token: "tok", Host: srv.URL, IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser(testUser)
	files := service.NewFileService(database)
	files.SetDataDir(t.TempDir())
	svc := New(database, api.NewClient(), files)

	// Round 1 succeeds so the note has cached content.
	if err := svc.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	assertReadableNote(t, database, srv.URL, "shared body")

	// Round 2 denies content; the client must revoke immediately and rebuild.
	server.mu.Lock()
	server.denyContent = true
	server.mu.Unlock()

	if err := svc.SyncOnce(); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	snapshots := server.snapshotHits
	server.mu.Unlock()
	if snapshots < 2 {
		t.Fatalf("expected a rebuild round after explicit revocation, got %d snapshot rounds", snapshots)
	}
	accountID := db.SharedAccountID(srv.URL, testUser)
	if note, _ := database.GetSharedNote(accountID, testNoteID); note != nil {
		t.Fatal("note should be revoked after explicit noPermission")
	}
}
