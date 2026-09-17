package sync

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

var imageFileIDRe = regexp.MustCompile(`fileId=([a-zA-Z0-9]{24})`)
var serverImageRe = regexp.MustCompile(`(https?://[^\s"']+)/api2/file/getImage\?fileId=([a-zA-Z0-9]{24})`)

func (s *SyncService) syncNotebooks(afterUsn int64, syncInfo *models.SyncInfo) error {
	logrus.Info("Syncing notebooks...")

	currentUsn := afterUsn
	remoteIDs := make(map[string]bool)

	for {
		notebooks, err := s.api.GetSyncNotebooks(currentUsn, s.maxEntry)
		if err != nil {
			return err
		}

		if len(notebooks) == 0 {
			break
		}

		for _, nb := range notebooks {
			// A complete snapshot is a union merge. A remote tombstone must not
			// erase the only remaining local copy after a database switch.
			if afterUsn < 0 && nb.IsDeleted {
				continue
			}
			if !nb.IsDeleted {
				remoteIDs[nb.NotebookID] = true
			}
			if err := s.processNotebookSync(nb, syncInfo); err != nil {
				return fmt.Errorf("process remote notebook %s: %w", nb.NotebookID, err)
			}
		}

		if len(notebooks) > 0 {
			currentUsn = notebooks[len(notebooks)-1].Usn
			if err := s.db.UpdateUserSyncState(s.db.GetCurrentUserID(), map[string]int64{
				"notebook_usn": currentUsn,
			}); err != nil {
				return err
			}
		}

		if len(notebooks) < s.maxEntry {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
	if afterUsn < 0 {
		logrus.Infof("Full notebook snapshot received: %d", len(remoteIDs))
		if err := s.db.ReconcileFullNotebooks(s.db.GetCurrentUserID(), remoteIDs); err != nil {
			return err
		}
		if err := s.db.RestoreNotebookParents(s.db.GetCurrentUserID()); err != nil {
			return err
		}
	}

	return nil
}

func (s *SyncService) processNotebookSync(serverNb *models.Notebook, syncInfo *models.SyncInfo) error {
	if serverNb == nil {
		return nil
	}
	// Keep parent references local while retaining ServerNotebookID for
	// uploads. The server sends ParentNotebookId values in its own ID space.
	if serverNb.ParentNotebookID != "" {
		if localParent, err := s.db.GetNotebookIDByServerID(serverNb.ParentNotebookID); err == nil && localParent != "" {
			copyNb := *serverNb
			copyNb.ParentNotebookID = localParent
			serverNb = &copyNb
		}
	}
	if serverNb.IsDeleted {
		localID, _ := s.db.GetNotebookIDByServerID(serverNb.NotebookID)
		if localID != "" {
			s.db.DeleteNotebookForce(localID)
			syncInfo.Notebook.Deletes = append(syncInfo.Notebook.Deletes, localID)
		}
		return nil
	}

	localNb, err := s.db.GetNotebookByServerID(serverNb.NotebookID)
	if err != nil {
		return err
	}

	if localNb == nil {
		created, err := s.db.AddNotebookForce(serverNb)
		if err != nil {
			return err
		}
		syncInfo.Notebook.Adds = append(syncInfo.Notebook.Adds, created.NotebookID)
		return nil
	}
	// UpdateNotebookForce addresses rows by the local notebook ID. Preserve
	// the remote ID only for lookup and translate the primary key before the
	// local update.
	serverNbCopy := *serverNb
	serverNbCopy.NotebookID = localNb.NotebookID
	serverNb = &serverNbCopy

	if localNb.Usn == serverNb.Usn {
		// USN equality only means the server did not advance its cursor; it
		// does not guarantee that this cache row has the same parent metadata.
		// This matters after switching databases: a server root (empty parent)
		// can still have a stale local self-parent and disappear from the tree.
		if !localNb.IsDirty && (localNb.NumberNotes != serverNb.NumberNotes ||
			localNb.ParentNotebookID != serverNb.ParentNotebookID ||
			localNb.Title != serverNb.Title || localNb.Seq != serverNb.Seq ||
			localNb.LocalIsDelete) {
			return s.db.UpdateNotebookForce(serverNb)
		}
		return nil
	}

	return s.db.UpdateNotebookForce(serverNb)
}

func (s *SyncService) syncNotes(afterUsn int64, syncInfo *models.SyncInfo) error {
	logrus.Info("Syncing notes...")

	currentUsn := afterUsn
	remoteIDs := make(map[string]bool)

	for {
		notes, err := s.api.GetSyncNotes(currentUsn, s.maxEntry)
		if err != nil {
			return err
		}

		if len(notes) == 0 {
			break
		}

		for _, note := range notes {
			if afterUsn < 0 && note.IsDeleted {
				continue
			}
			if !note.IsDeleted {
				remoteIDs[note.NoteID] = true
			}
			if err := s.processNoteSync(note, syncInfo); err != nil {
				return fmt.Errorf("process remote note %s: %w", note.NoteID, err)
			}
		}

		if len(notes) > 0 {
			currentUsn = notes[len(notes)-1].Usn
			if err := s.db.UpdateUserSyncState(s.db.GetCurrentUserID(), map[string]int64{"note_usn": currentUsn}); err != nil {
				return err
			}
		}

		if len(notes) < s.maxEntry {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
	if afterUsn < 0 {
		logrus.Infof("Full note snapshot received: %d", len(remoteIDs))
		if err := s.db.RequeueMissingDesktopNotes(s.db.GetCurrentUserID(), remoteIDs); err != nil {
			return err
		}
	}

	return nil
}

func (s *SyncService) processNoteSync(serverNote *models.Note, syncInfo *models.SyncInfo) error {
	remoteNoteID := serverNote.NoteID
	if serverNote.IsDeleted {
		localID, _ := s.db.GetLocalNoteID(serverNote.NoteID)
		if localID != "" {
			s.deleteNoteFiles(localID)
			s.db.DeleteNoteForce(localID)
			syncInfo.Note.Deletes = append(syncInfo.Note.Deletes, localID)
		}
		return nil
	}
	// Notes received from the server reference notebooks by remote ID. The
	// desktop cache keeps its own local notebook IDs, so translate before any
	// insert/update; otherwise moved notes appear under the wrong notebook and
	// notebook counters no longer match the local note rows.
	if serverNote.NotebookID != "" {
		if localNotebookID, err := s.db.GetNotebookIDByServerID(serverNote.NotebookID); err == nil && localNotebookID != "" {
			copyNote := *serverNote
			copyNote.NotebookID = localNotebookID
			serverNote = &copyNote
		}
	}

	localNote, err := s.db.GetNoteByServerID(serverNote.NoteID)
	if err != nil {
		return err
	}
	if localNote == nil {
		// A cache written by an older client may have lost only the remote
		// mapping. Match the identical ID before inserting another local row.
		byLocalID, err := s.db.GetNote(serverNote.NoteID)
		if err != nil {
			return err
		}
		if byLocalID != nil && byLocalID.ServerNoteID == "" {
			if err := s.db.SetServerNoteID(byLocalID.NoteID, serverNote.NoteID); err != nil {
				return err
			}
			byLocalID.ServerNoteID = serverNote.NoteID
			localNote = byLocalID
		} else if byLocalID != nil {
			// Same local ID, different remote object: preserve both notes.
			copyNote := *serverNote
			copyNote.ServerNoteID = serverNote.NoteID
			copyNote.NoteID = utils.ObjectId()
			serverNote = &copyNote
		}
	}

	if localNote == nil {
		created, err := s.db.AddNoteForce(serverNote)
		if err != nil {
			return err
		}
		if created != nil {
			syncInfo.Note.Adds = append(syncInfo.Note.Adds, created.NoteID)
			if err := s.syncNoteContentAndFiles(created); err != nil {
				return err
			}
		}
		return nil
	}
	// Keep the local primary key when updating an existing cached note. The
	// server NoteId is only the mapping key; using it in UPDATE ... WHERE
	// note_id would silently update zero rows and leave moves/content stale.
	serverNoteCopy := *serverNote
	serverNoteCopy.NoteID = localNote.NoteID
	serverNote = &serverNoteCopy

	if localNote.Usn == serverNote.Usn {
		if serverNote.NotebookID != "" && localNote.NotebookID != serverNote.NotebookID {
			if err := s.db.SetNoteNotebook(localNote.NoteID, serverNote.NotebookID); err != nil {
				return err
			}
			syncInfo.Note.Updates = append(syncInfo.Note.Updates, localNote.NoteID)
		}
		// Older desktop builds could erase cached content while applying a
		// metadata-only update (for example, changing IsStar). A forced full
		// sync must be able to repair those already-corrupted cache rows.
		if localNote.Content == "" && localNote.ServerNoteID != "" {
			if err := s.syncNoteContentAndFiles(localNote); err != nil {
				return err
			}
		}
		if localNote.LocalIsDelete {
			if err := s.db.UpdateNoteForce(serverNote, false); err != nil {
				return err
			}
		}
		return nil
	}
	if !serverNote.IsStarPresent {
		serverNote.IsStar = localNote.IsStar
	}

	if localNote.IsDirty {
		serverContent, err := s.api.GetNoteContent(remoteNoteID)
		if err != nil {
			return err
		}

		if serverContent == localNote.Content {
			return s.db.UpdateNoteForce(serverNote, false)
		}

		conflictCopy, err := s.db.CopyNoteForConflict(localNote.NoteID)
		if err != nil {
			logrus.Errorf("Copy conflict note error: %v", err)
		}
		if conflictCopy != nil {
			syncInfo.Note.Conflicts = append(syncInfo.Note.Conflicts, &models.SyncConflict{
				Server:       serverNote,
				Local:        localNote,
				ConflictCopy: conflictCopy,
			})
		}
		return nil
	}

	err = s.db.UpdateNoteForce(serverNote, true)
	if err == nil && (serverNote.InitSync || localNote.Content == "") {
		err = s.syncNoteContentAndFiles(localNote)
	}
	return err
}

func (s *SyncService) syncNoteContentAndFiles(note *models.Note) error {
	if note == nil || note.ServerNoteID == "" {
		return fmt.Errorf("cannot sync note content without a server note ID")
	}

	content, err := s.api.GetNoteContent(note.ServerNoteID)
	if err != nil {
		return fmt.Errorf("get note content %s: %w", note.NoteID, err)
	}

	user, _ := s.db.GetActiveUser()
	if user != nil && user.Host != "" {
		localPrefix := "/api2/file/getImage"
		content = utils.FixNoteContent(content, user.Host, localPrefix)
	}

	s.downloadContentImages(content)

	return s.db.UpdateNoteContent(note.NoteID, content)
}

func (s *SyncService) downloadContentImages(content string) {
	if content == "" {
		return
	}

	matches := serverImageRe.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			fileID := match[2]
			s.downloadImageFromServer(fileID)
		}
	}

	localMatches := imageFileIDRe.FindAllStringSubmatch(content, -1)
	for _, match := range localMatches {
		if len(match) >= 2 {
			fileID := match[1]
			s.downloadImageFromServer(fileID)
		}
	}
}

func (s *SyncService) downloadImageFromServer(fileID string) {
	img, _ := s.db.GetImage(fileID)
	if img != nil && img.Path != "" {
		return
	}

	img, _ = s.db.GetImageByServerID(fileID)
	if img != nil && img.Path != "" {
		return
	}

	localPath, err := s.api.GetImage(fileID, s.files.GetImageDir())
	if err != nil {
		logrus.Errorf("Download image %s error: %v", fileID, err)
		return
	}

	if localPath == "" {
		return
	}

	s.files.AddImageForce(fileID, localPath)
}

func (s *SyncService) downloadAttachFromServer(serverFileID, noteID string) {
	attach, _ := s.db.GetAttachByServerID(serverFileID)
	if attach != nil && attach.Path != "" {
		return
	}

	localPath, filename, err := s.api.GetAttach(serverFileID, s.files.GetAttachDir())
	if err != nil {
		logrus.Errorf("Download attach %s error: %v", serverFileID, err)
		return
	}

	if localPath == "" {
		return
	}

	userID := s.db.GetCurrentUserID()
	now := time.Now()
	newAttach := &models.Attach{
		ID:           utils.ObjectId(),
		FileID:       utils.ObjectId(),
		ServerFileID: serverFileID,
		NoteID:       noteID,
		UserID:       userID,
		Title:        filename,
		Path:         localPath,
		IsAttach:     true,
		IsDirty:      false,
		CreatedTime:  &now,
	}
	s.db.InsertAttach(newAttach)
}

func (s *SyncService) deleteNoteFiles(noteID string) {
	attachs, err := s.db.GetAttachsByNote(noteID)
	if err == nil {
		for _, att := range attachs {
			if att.Path != "" {
				strings.Contains(att.Path, s.files.GetAttachDir())
			}
			s.db.DeleteAttach(att.FileID)
		}
	}
}

func (s *SyncService) syncImagesAndAttachs(syncInfo *models.SyncInfo) error {
	logrus.Info("Syncing images and attachments...")

	notes := syncInfo.Note.Adds
	notes = append(notes, syncInfo.Note.Updates...)

	for i, noteID := range notes {
		note, err := s.db.GetNote(noteID)
		if err != nil || note == nil {
			continue
		}

		if note.Content != "" {
			s.downloadContentImages(note.Content)
		}

		if note.Attachs != nil {
			for _, att := range note.Attachs {
				if att.ServerFileID != "" {
					s.downloadAttachFromServer(att.ServerFileID, note.NoteID)
				}
			}
		}

		s.emitProgress(fmt.Sprintf("note %d/%d", i+1, len(notes)), i+1, len(notes))
	}

	return nil
}

func (s *SyncService) syncTags(afterUsn int64, syncInfo *models.SyncInfo) error {
	logrus.Info("Syncing tags...")

	currentUsn := afterUsn

	for {
		tags, err := s.api.GetSyncTags(currentUsn, s.maxEntry)
		if err != nil {
			return err
		}

		if len(tags) == 0 {
			break
		}

		for _, tag := range tags {
			if err := s.processTagSync(tag, syncInfo); err != nil {
				logrus.Errorf("Process tag error: %v", err)
			}
		}

		if len(tags) > 0 {
			currentUsn = tags[len(tags)-1].Usn
			s.db.UpdateUserSyncState(s.db.GetCurrentUserID(), map[string]int64{"tag_usn": currentUsn})
		}

		if len(tags) < s.maxEntry {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}
	if afterUsn < 0 {
		if err := s.db.CleanupUnusedTags(s.db.GetCurrentUserID()); err != nil {
			return err
		}
	}

	return nil
}

func (s *SyncService) processTagSync(serverTag *models.Tag, syncInfo *models.SyncInfo) error {
	userID := s.db.GetCurrentUserID()

	if serverTag.LocalIsDelete {
		s.db.DeleteTagForce(serverTag.Tag)
		syncInfo.Tag.Deletes = append(syncInfo.Tag.Deletes, serverTag.Tag)
		return nil
	}

	tag, err := s.db.GetTag(userID, serverTag.Tag)
	if err != nil {
		return err
	}

	if tag == nil {
		_, err := s.db.AddOrUpdateTag(userID, serverTag.Tag, true, serverTag.Usn)
		if err != nil {
			return err
		}
		syncInfo.Tag.Adds = append(syncInfo.Tag.Adds, serverTag.Tag)
		return nil
	}

	return s.db.SetTagNotDirtyAndUsn(serverTag.Tag, serverTag.Usn)
}
