package sharedsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"pearlnote/api"
	"pearlnote/db"
	"pearlnote/models"
	"pearlnote/service"
)

const (
	maxSharedFileSize      = 100 << 20
	sharedCacheMaxBytes    = 2 << 30
	maxDownloadConcurrency = 3
	probeBackoffSeconds    = 30 * 60
)

type Service struct {
	db    *db.Database
	api   *api.Client
	files *service.FileService

	mu      sync.Mutex
	syncing bool

	OnRevocation func(noteIDs []string)
}

func New(database *db.Database, client *api.Client, files *service.FileService) *Service {
	return &Service{db: database, api: client, files: files}
}

// SyncOnce refreshes shared permissions under a lock so that at most one
// snapshot round per account runs at any time.
func (s *Service) SyncOnce() error {
	return s.withLock(func() error {
		user, err := s.db.GetActiveUser()
		if err != nil || user == nil {
			return err
		}
		if user.Token == "" || user.IsLocal {
			return nil
		}
		return s.round(user, 0)
	})
}

func (s *Service) withLock(run func() error) error {
	s.mu.Lock()
	if s.syncing {
		s.mu.Unlock()
		return nil
	}
	s.syncing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.syncing = false
		s.mu.Unlock()
	}()

	return run()
}

// DownloadPending processes images and attachments explicitly queued by the
// local UI without waiting for the next full snapshot refresh.
func (s *Service) DownloadPending() error {
	return s.withLock(func() error {
		user, err := s.db.GetActiveUser()
		if err != nil || user == nil {
			return err
		}
		if user.Token == "" || user.IsLocal {
			return nil
		}
		accountID := db.SharedAccountID(user.Host, user.ID)
		s.api.SetHost(user.Host)
		s.api.SetToken(user.Token)
		return s.downloadSharedFiles(accountID, user)
	})
}

// Sync refreshes shared permissions independently from personal USNs.
// A failed or incomplete snapshot leaves the last published cache untouched.
func (s *Service) Sync(user *models.User) error {
	return s.round(user, 0)
}

func (s *Service) round(user *models.User, attempt int) error {
	accountID := db.SharedAccountID(user.Host, user.ID)

	if s.db.SharedProbePaused(accountID) {
		return nil
	}

	generation, err := s.db.CurrentSharedGeneration(accountID)
	if err != nil {
		return err
	}
	if err = s.db.SavePendingSharedGeneration(accountID, generation); err != nil {
		return err
	}

	s.api.SetHost(user.Host)
	s.api.SetToken(user.Token)
	capability, err := s.api.GetSharedCapabilities()
	if err != nil {
		state := "error"
		if api.IsSharedUnsupported(err) {
			state = "unsupported"
		}
		_ = s.db.EnsureSharedAccount(accountID, user.Host, user.ID, 0, state)
		if state == "unsupported" {
			_ = s.db.SetSharedProbeBackoff(accountID, timeNow()+probeBackoffSeconds)
		}
		return err
	}
	if err = s.db.EnsureSharedAccount(accountID, user.Host, user.ID, capability.ProtocolVersion, "supported"); err != nil {
		return err
	}
	_ = s.db.SetSharedProbeBackoff(accountID, 0)

	snapshot, err := s.api.CreateSharedSnapshot()
	if err != nil {
		return err
	}
	if err = s.db.BeginSharedSnapshotStaging(accountID, snapshot.SnapshotID, snapshot.Total); err != nil {
		return err
	}

	token := ""
	seenTokens := map[string]bool{}
	for {
		page, err := s.api.GetSharedSnapshotPage(snapshot.SnapshotID, token)
		if err != nil {
			return err
		}
		if err := s.db.SaveSharedSnapshotStagingPage(accountID, snapshot.SnapshotID, page.NextPageToken, page.Items); err != nil {
			return err
		}
		if page.Complete {
			if page.NextPageToken != "" {
				return fmt.Errorf("complete shared snapshot has next page")
			}
			break
		}
		if page.NextPageToken == "" || seenTokens[page.NextPageToken] {
			return fmt.Errorf("invalid shared snapshot pagination")
		}
		seenTokens[page.NextPageToken] = true
		token = page.NextPageToken
	}

	if current, _ := s.db.GetActiveUser(); current == nil || db.SharedAccountID(current.Host, current.ID) != accountID {
		return fmt.Errorf("account switched during shared sync")
	}

	items, err := s.db.ReadSharedSnapshotStaging(accountID, snapshot.SnapshotID)
	if err != nil {
		return err
	}
	if snapshot.Total >= 0 && len(items) != snapshot.Total {
		return fmt.Errorf("shared snapshot count mismatch: got %d want %d", len(items), snapshot.Total)
	}
	if err = s.db.PublishSharedSnapshot(accountID, items, snapshot.Total); err != nil {
		return err
	}
	if err = s.db.ClearSharedSnapshotStaging(accountID, snapshot.SnapshotID); err != nil {
		return err
	}

	s.reclaimCache(accountID)

	contentErr := s.downloadSharedContent(accountID, user)
	fileErr := s.downloadSharedFiles(accountID, user)

	permissionLoss := s.revokeOnPermissionLoss(accountID)
	if fileErr != nil && strings.Contains(fileErr.Error(), "noPermission") {
		permissionLoss = true
	}
	if revoked, _ := s.db.RecentlyRevokedSharedNotes(accountID, generation+1); len(revoked) > 0 && s.OnRevocation != nil {
		s.OnRevocation(revoked)
	}
	if permissionLoss && attempt < 1 {
		logrus.Warnf("explicit revocation during shared sync, rebuilding snapshot")
		return s.round(user, attempt+1)
	}

	if contentErr != nil {
		logrus.Warnf("Shared content download failed: %v", contentErr)
	}
	if fileErr != nil {
		logrus.Warnf("Shared file download failed: %v", fileErr)
	}
	return nil
}

// revokeOnPermissionLoss marks notes whose download was explicitly denied as
// revoked right away; it reports whether any such denial happened.
func (s *Service) revokeOnPermissionLoss(accountID string) bool {
	jobs, err := s.db.FailedSharedContentJobs(accountID)
	if err != nil {
		return false
	}
	revokedAny := false
	var revoked []string
	for _, job := range jobs {
		if !strings.Contains(job.Error, "noPermission") {
			continue
		}
		if err := s.db.RevokeSharedNote(accountID, job.ResourceID); err == nil {
			revokedAny = true
			revoked = append(revoked, job.ResourceID)
		}
	}
	if len(revoked) > 0 && s.OnRevocation != nil {
		s.OnRevocation(revoked)
	}
	return revokedAny
}

func (s *Service) reclaimCache(accountID string) {
	paths, err := s.db.ListRevokedSharedFilePaths(accountID)
	if err == nil {
		for _, path := range paths {
			os.Remove(path)
		}
		s.db.ClearRevokedSharedFilePaths(accountID)
	}
	cleanTempFiles(filepath.Join(s.files.GetDataDir(), "shared", accountID, "files"))
	evictSharedAttachmentsOverCap(s.db, accountID, sharedCacheMaxBytes)
}

func cleanTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func evictSharedAttachmentsOverCap(database *db.Database, accountID string, capBytes int64) {
	files, err := database.ReadySharedAttachmentsByAge(accountID)
	if err != nil {
		return
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	if total <= capBytes {
		return
	}
	for _, f := range files {
		if total <= capBytes {
			return
		}
		if f.LocalPath != "" {
			os.Remove(f.LocalPath)
		}
		database.MarkSharedFilePending(accountID, f)
		total -= f.Size
	}
}

func (s *Service) downloadSharedContent(accountID string, user *models.User) error {
	jobs, err := s.db.PendingSharedContentJobs(accountID, 200)
	if err != nil {
		return err
	}
	var failures []string
	for _, job := range jobs {
		if current, _ := s.db.GetActiveUser(); current == nil || db.SharedAccountID(current.Host, current.ID) != accountID {
			return fmt.Errorf("account switched during shared download")
		}
		content, e := s.api.GetSharedNoteContent(job[0])
		if e == nil && content.NoteID != job[0] {
			e = fmt.Errorf("shared content note mismatch")
		}
		if e == nil && job[1] != "" && content.Version != job[1] {
			e = fmt.Errorf("shared content version mismatch")
		}
		if e == nil {
			sum := sha256.Sum256([]byte(content.Content))
			actual := hex.EncodeToString(sum[:])
			if content.Version != actual || (content.Digest != "" && content.Digest != actual) {
				e = fmt.Errorf("shared content digest mismatch")
			}
		}
		if e != nil {
			_ = s.db.FailSharedContent(accountID, job[0], job[1], e.Error())
			failures = append(failures, job[0]+": "+e.Error())
			continue
		}
		if e = s.db.CompleteSharedContent(accountID, job[0], job[1], content.Version, content.Digest, content.Content); e != nil {
			failures = append(failures, job[0]+": "+e.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("shared content failures: %s", strings.Join(failures, "; "))
	}
	return nil
}

// downloadSharedFiles eagerly fetches referenced images (bounded concurrency);
// attachments stay pending until they are queued for offline use.
func (s *Service) downloadSharedFiles(accountID string, user *models.User) error {
	jobs, err := s.db.PendingSharedFileJobs(accountID, []string{"image", "attachment"}, 200)
	if err != nil {
		return err
	}
	sem := make(chan struct{}, maxDownloadConcurrency)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)
	for _, job := range jobs {
		if current, _ := s.db.GetActiveUser(); current == nil || db.SharedAccountID(current.Host, current.ID) != accountID {
			return fmt.Errorf("account switched during shared download")
		}
		if job.Size > maxSharedFileSize {
			_ = s.db.FailSharedFile(accountID, job, "shared file exceeds size limit")
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(job *models.SharedFile) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if e := s.fetchSharedFile(accountID, user, job); e != nil {
				mu.Lock()
				failures = append(failures, job.FileID+": "+e.Error())
				mu.Unlock()
			}
		}(job)
	}
	wg.Wait()
	if len(failures) > 0 {
		return fmt.Errorf("shared file failures: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (s *Service) fetchSharedFile(accountID string, user *models.User, job *models.SharedFile) error {
	body, digest, e := s.api.GetSharedNoteFile(job.NoteID, job.FileID)
	if api.IsSharedPermissionDenied(e) {
		_ = s.db.RevokeSharedNote(accountID, job.NoteID)
		if s.OnRevocation != nil {
			s.OnRevocation([]string{job.NoteID})
		}
	}
	if e == nil && job.TargetVersion != "" && digest != "" && digest != job.TargetVersion {
		e = fmt.Errorf("shared file digest mismatch")
	}
	if e == nil {
		sum := sha256.Sum256(body)
		actual := hex.EncodeToString(sum[:])
		if digest != "" && digest != actual {
			e = fmt.Errorf("shared file digest mismatch")
		}
		if job.TargetVersion != "" && digest == "" && job.TargetVersion != actual {
			e = fmt.Errorf("shared file version mismatch")
		}
	}
	if e == nil && len(body) > maxSharedFileSize {
		e = fmt.Errorf("shared file exceeds size limit")
	}
	if e != nil {
		_ = s.db.FailSharedFile(accountID, job, e.Error())
		return e
	}
	localPath := filepath.Join(s.files.GetDataDir(), "shared", accountID, "files", job.FileID)
	if e := storeSharedFile(localPath, body); e != nil {
		_ = s.db.FailSharedFile(accountID, job, e.Error())
		return e
	}
	return s.db.CompleteSharedFile(accountID, job, localPath)
}

func storeSharedFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func timeNow() int64 {
	return time.Now().Unix()
}
