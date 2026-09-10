package sync

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"pearlnote/api"
	"pearlnote/db"
	"pearlnote/models"
	"pearlnote/service"
)

type SyncService struct {
	db       *db.Database
	api      *api.Client
	files    *service.FileService
	maxEntry int

	mu            sync.Mutex
	isSyncing     bool
	needSyncAgain bool
	retryCount    int
	progressCb    func(stage string, current, total int)
}

func NewSyncService(database *db.Database, client *api.Client) *SyncService {
	return &SyncService{
		db:       database,
		api:      client,
		files:    service.NewFileService(database),
		maxEntry: 200,
	}
}

func (s *SyncService) SetProgressCallback(cb func(stage string, current, total int)) {
	s.progressCb = cb
}

func (s *SyncService) emitProgress(stage string, current, total int) {
	if s.progressCb != nil {
		s.progressCb(stage, current, total)
	}
}

func (s *SyncService) FullSync() (*models.SyncInfo, error) {
	s.mu.Lock()
	if s.isSyncing {
		s.mu.Unlock()
		return nil, ErrAlreadySyncing
	}
	s.isSyncing = true
	s.needSyncAgain = false
	s.retryCount = 0
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isSyncing = false
		s.mu.Unlock()
	}()

	logrus.Info("Starting full sync...")
	s.emitProgress("start", 0, 100)

	syncInfo := models.NewSyncInfo()

	user, err := s.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, err
	}

	s.db.SetCurrentUser(user.ID)
	s.api.SetHost(user.Host)
	s.api.SetToken(user.Token)

	lastUsn, notebookUsn, noteUsn, tagUsn, err := s.db.GetAllLastSyncState(user.ID)
	if err != nil {
		return nil, err
	}

	serverState, err := s.api.GetLastSyncState()
	if err != nil {
		return nil, err
	}

	logrus.Debugf("Server LastSyncUsn: %d, Local: %d", serverState.LastSyncUsn, lastUsn)

	s.emitProgress("notebooks", 0, 100)
	if err := s.syncNotebooks(notebookUsn, syncInfo); err != nil {
		logrus.Errorf("Sync notebooks error: %v", err)
		return nil, err
	}

	s.emitProgress("notes", 20, 100)
	if err := s.syncNotes(noteUsn, syncInfo); err != nil {
		logrus.Errorf("Sync notes error: %v", err)
		return nil, err
	}

	s.emitProgress("tags", 40, 100)
	if err := s.syncTags(tagUsn, syncInfo); err != nil {
		logrus.Errorf("Sync tags error: %v", err)
		return nil, err
	}

	s.emitProgress("push", 60, 100)
	if err := s.sendChanges(syncInfo); err != nil {
		logrus.Errorf("Send changes error: %v", err)
		return nil, err
	}

	s.emitProgress("images", 80, 100)
	if err := s.syncImagesAndAttachs(syncInfo); err != nil {
		logrus.Errorf("Sync images/attachs error: %v", err)
		return nil, err
	}

	if err := s.db.UpdateUserSyncState(user.ID, map[string]int64{
		"last_sync_usn":  serverState.LastSyncUsn,
		"last_sync_time": time.Now().Unix(),
	}); err != nil {
		return nil, err
	}

	s.emitProgress("done", 100, 100)
	logrus.Info("Full sync completed")
	return syncInfo, nil
}

func (s *SyncService) IncrSync() (*models.SyncInfo, error) {
	s.mu.Lock()
	if s.isSyncing {
		s.mu.Unlock()
		return nil, ErrAlreadySyncing
	}
	s.isSyncing = true
	s.needSyncAgain = false
	s.retryCount = 0
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isSyncing = false
		s.mu.Unlock()
	}()

	logrus.Info("Starting incremental sync...")

	syncInfo := models.NewSyncInfo()

	user, err := s.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, err
	}

	s.db.SetCurrentUser(user.ID)
	s.api.SetHost(user.Host)
	s.api.SetToken(user.Token)

	lastUsn, _, _, _, err := s.db.GetAllLastSyncState(user.ID)
	if err != nil {
		return nil, err
	}

	serverState, err := s.api.GetLastSyncState()
	if err != nil {
		return nil, err
	}

	if serverState.LastSyncUsn > lastUsn {
		logrus.Debugf("Server has updates, pulling...")

		if err := s.syncNotebooks(lastUsn, syncInfo); err != nil {
			logrus.Errorf("Sync notebooks error: %v", err)
		}

		if err := s.syncNotes(lastUsn, syncInfo); err != nil {
			logrus.Errorf("Sync notes error: %v", err)
		}

		if err := s.syncTags(lastUsn, syncInfo); err != nil {
			logrus.Errorf("Sync tags error: %v", err)
		}
	}

	if err := s.sendChanges(syncInfo); err != nil {
		logrus.Errorf("Send changes error: %v", err)
	}

	if err := s.syncImagesAndAttachs(syncInfo); err != nil {
		logrus.Errorf("Sync images/attachs error: %v", err)
	}

	s.mu.Lock()
	needRetry := s.needSyncAgain && s.retryCount < 5
	s.mu.Unlock()

	if needRetry {
		s.mu.Lock()
		s.retryCount++
		s.mu.Unlock()
		logrus.Info("Need to sync again...")
		return s.IncrSync()
	}

	logrus.Info("Incremental sync completed")
	return syncInfo, nil
}

func (s *SyncService) ForceFullSync() error {
	user, err := s.db.GetActiveUser()
	if err != nil || user == nil {
		return err
	}

	s.db.UpdateUserSyncState(user.ID, map[string]int64{
		"last_sync_usn": -1,
		"notebook_usn":  -1,
		"note_usn":      -1,
		"tag_usn":       -1,
	})

	_, err = s.FullSync()
	return err
}

func (s *SyncService) IsSyncing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isSyncing
}

func (s *SyncService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.needSyncAgain = false
}

var ErrAlreadySyncing = &SyncError{Msg: "already syncing"}

type SyncError struct {
	Msg string
}

func (e *SyncError) Error() string {
	return e.Msg
}

func (s *SyncService) checkNeedSyncAgain(usn int64) {
	user, _ := s.db.GetActiveUser()
	if user == nil {
		return
	}

	lastUsn, _, _, _, _ := s.db.GetAllLastSyncState(user.ID)

	if usn != lastUsn+1 {
		s.mu.Lock()
		s.needSyncAgain = true
		s.mu.Unlock()
	} else {
		s.db.UpdateLastSyncUsn(user.ID, usn)
	}
}
