package sync

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/gemsnote/gemsnote/api"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

var localImageRe = regexp.MustCompile(`(?:leanote://file/getImage|/api2/file/getImage)\?fileId=([a-zA-Z0-9]{24})`)

func (s *SyncService) sendChanges(syncInfo *models.SyncInfo) error {
	logrus.Info("Sending changes...")

	userID := s.db.GetCurrentUserID()
	var firstErr error

	if err := s.sendNotebookChanges(userID, syncInfo); err != nil {
		logrus.Errorf("Send notebook changes error: %v", err)
		firstErr = err
	}

	if err := s.sendNoteChanges(userID, syncInfo); err != nil {
		logrus.Errorf("Send note changes error: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	if err := s.sendTagChanges(userID, syncInfo); err != nil {
		logrus.Errorf("Send tag changes error: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (s *SyncService) sendNotebookChanges(userID string, syncInfo *models.SyncInfo) error {
	notebooks, err := s.db.GetDirtyNotebooks(userID)
	if err != nil {
		return err
	}
	// Recreated notebook trees must be uploaded parent-first, so the child's
	// ParentNotebookId can be translated to the newly assigned server ID.
	byID := make(map[string]*models.Notebook, len(notebooks))
	for _, nb := range notebooks {
		byID[nb.NotebookID] = nb
	}
	// Topologically order the dirty tree. A simple depth sort can still leave
	// siblings in the wrong order when legacy parent IDs are malformed.
	ordered := make([]*models.Notebook, 0, len(notebooks))
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(*models.Notebook)
	visit = func(nb *models.Notebook) {
		if nb == nil || visited[nb.NotebookID] {
			return
		}
		if visiting[nb.NotebookID] {
			return
		} // malformed cycle; sync will promote it
		visiting[nb.NotebookID] = true
		visit(byID[nb.ParentNotebookID])
		delete(visiting, nb.NotebookID)
		visited[nb.NotebookID] = true
		ordered = append(ordered, nb)
	}
	for _, nb := range notebooks {
		visit(nb)
	}
	notebooks = ordered

	var firstErr error
	for _, nb := range notebooks {
		if nb.LocalIsNew && nb.LocalIsDelete {
			s.db.DeleteLocalNotebook(nb.NotebookID)
			continue
		}

		var serverNb *models.Notebook
		var apiErr error

		if nb.LocalIsNew {
			serverNb, apiErr = s.api.AddNotebook(s.prepareNotebookForUpload(nb))
		} else if nb.LocalIsDelete {
			var resp *api.APIResponse
			resp, apiErr = s.api.DeleteNotebook(nb)
			if resp != nil && resp.Ok {
				s.db.SetNotebookNotDirty(nb.NotebookID)
			} else if apiErr == nil {
				apiErr = fmt.Errorf("delete notebook failed")
			}
			if apiErr != nil && firstErr == nil {
				firstErr = apiErr
			}
			continue
		} else {
			serverNb, apiErr = s.api.UpdateNotebook(s.prepareNotebookForUpload(nb))
		}

		if apiErr != nil {
			logrus.Errorf("API error for notebook %s: %v", nb.NotebookID, apiErr)
			if firstErr == nil {
				firstErr = apiErr
			}
			continue
		}

		if serverNb != nil {
			s.db.UpdateNotebookAfterSync(nb.NotebookID, serverNb)
			if nb.LocalIsNew {
				syncInfo.Notebook.ChangeAdds = append(syncInfo.Notebook.ChangeAdds, serverNb.NotebookID)
			} else {
				syncInfo.Notebook.ChangeUpdates = append(syncInfo.Notebook.ChangeUpdates, serverNb.NotebookID)
			}
			s.checkNeedSyncAgain(serverNb.Usn)
		}
	}

	return firstErr
}

func (s *SyncService) sendNoteChanges(userID string, syncInfo *models.SyncInfo) error {
	notes, err := s.db.GetDirtyNotes(userID)
	if err != nil {
		return err
	}

	var firstErr error
	for _, note := range notes {
		if note.InitSync {
			if note.LocalIsNew {
				err := fmt.Errorf("note %s has no complete local content cache; cannot merge safely", note.NoteID)
				if firstErr == nil {
					firstErr = err
				}
				logrus.Error(err)
			}
			continue
		}

		if note.ConflictNoteID != "" && !note.ConflictFixed {
			logrus.Debugf("Skip unfixed conflict note: %s", note.NoteID)
			continue
		}

		// Older caches (and caches reused after switching servers) can contain
		// a note that was never mapped to a remote NoteId. Treat it as a new
		// remote note instead of sending an empty NoteId to updateNote.
		if note.LocalIsNew || note.ServerNoteID == "" {
			if !note.IsTrash && !note.LocalIsDelete {
				noteCopy := s.prepareNoteForUpload(note)
				serverNote, apiErr := s.api.AddNote(noteCopy)
				if apiErr != nil {
					logrus.Errorf("Add note error: %v", apiErr)
					if firstErr == nil {
						firstErr = apiErr
					}
					syncInfo.Note.Errors = append(syncInfo.Note.Errors, &models.SyncError{
						Err:  apiErr.Error(),
						Note: note,
					})
					continue
				}

				if serverNote != nil {
					s.processNoteAfterSync(note, serverNote, true)
					syncInfo.Note.ChangeAdds = append(syncInfo.Note.ChangeAdds, serverNote.NoteID)
					s.checkNeedSyncAgain(serverNote.Usn)
				}
			} else if note.LocalIsDelete {
				s.db.DeleteLocalNote(note.NoteID)
			}
		} else if note.LocalIsDelete {
			resp, apiErr := s.api.DeleteTrash(note)
			if apiErr != nil {
				logrus.Errorf("Delete note error: %v", apiErr)
				if firstErr == nil {
					firstErr = apiErr
				}
				continue
			}

			if resp.Ok {
				s.db.DeleteLocalNote(note.NoteID)
				s.deleteNoteFiles(note.NoteID)
				s.checkNeedSyncAgain(resp.Usn)
			} else if resp.Msg == "notExists" {
				s.db.DeleteLocalNote(note.NoteID)
			}
		} else {
			noteCopy := s.prepareNoteForUpload(note)
			serverNote, apiErr := s.api.UpdateNote(noteCopy)
			if apiErr != nil {
				// A valid local cache may still point at a note from a previous
				// server installation. Re-create it remotely when the server
				// explicitly says that the NoteId is unknown.
				if isMissingRemoteNote(apiErr) {
					if recreated, localNotebookID, addErr := s.recreateMissingNote(note); addErr == nil && recreated != nil {
						if localNotebookID != "" && localNotebookID != note.NotebookID {
							_ = s.db.SetNoteNotebook(note.NoteID, localNotebookID)
							note.NotebookID = localNotebookID
						}
						s.processNoteAfterSync(note, recreated, false)
						syncInfo.Note.ChangeAdds = append(syncInfo.Note.ChangeAdds, recreated.NoteID)
						s.checkNeedSyncAgain(recreated.Usn)
						continue
					} else if addErr != nil {
						logrus.Errorf("Re-create note %s after missing remote note failed: %v", note.NoteID, addErr)
					}
				}
				logrus.Errorf("Update note error: %v", apiErr)
				if firstErr == nil {
					firstErr = apiErr
				}
				syncInfo.Note.Errors = append(syncInfo.Note.Errors, &models.SyncError{
					Err:  apiErr.Error(),
					Note: note,
				})
				continue
			}

			if serverNote != nil {
				s.processNoteAfterSync(note, serverNote, false)
				syncInfo.Note.ChangeUpdates = append(syncInfo.Note.ChangeUpdates, serverNote.NoteID)
				s.checkNeedSyncAgain(serverNote.Usn)
			}
		}
	}

	return firstErr
}

func isMissingRemoteNote(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "noteidnotexists") || strings.Contains(msg, "notexists")
}

func (s *SyncService) recreateMissingNote(note *models.Note) (*models.Note, string, error) {
	if note == nil {
		return nil, "", fmt.Errorf("missing local note")
	}
	userID := s.db.GetCurrentUserID()
	type candidate struct{ localID, serverID string }
	var candidates []candidate
	seen := map[string]bool{}
	addCandidate := func(localID, serverID string) {
		if serverID == "" || seen[serverID] {
			return
		}
		seen[serverID] = true
		candidates = append(candidates, candidate{localID: localID, serverID: serverID})
	}
	if nb, err := s.db.GetNotebook(note.NotebookID); err == nil && nb != nil {
		addCandidate(nb.NotebookID, nb.ServerNotebookID)
	}
	// The local row may itself contain a server ID. Try it after the explicit
	// notebook mapping, then fall back to any current notebook for this user.
	addCandidate(note.NotebookID, note.NotebookID)
	if notebooks, err := s.db.GetNotebooks(userID); err == nil {
		for _, nb := range notebooks {
			if nb != nil && !nb.IsTrash {
				addCandidate(nb.NotebookID, nb.ServerNotebookID)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("no notebook available for note recreation")
	}
	var lastErr error
	for _, c := range candidates {
		copyNote := *note
		copyNote.ServerNoteID = ""
		copyNote.LocalIsNew = true
		copyNote.NotebookID = c.serverID
		created, err := s.api.AddNote(s.prepareNoteForUpload(&copyNote))
		if err == nil && created != nil {
			return created, c.localID, nil
		}
		lastErr = err
		logrus.Warnf("Re-create note %s using notebook %s failed: %v", note.NoteID, c.serverID, err)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("server returned an empty note")
	}
	return nil, "", lastErr
}

func (s *SyncService) prepareNoteForUpload(note *models.Note) *models.Note {
	noteCopy := *note
	// Local notebook IDs are intentionally independent from server IDs. API2
	// expects NotebookId to reference the remote notebook, so translate the
	// mapping before sending a note (otherwise the server reports
	// notebookIdNotExists after a fresh login or server switch).
	if note.NotebookID != "" {
		nb, err := s.db.GetNotebook(note.NotebookID)
		if err == nil && nb == nil {
			// Some imported caches already store the remote ID in notebook_id.
			nb, err = s.db.GetNotebookByServerID(note.NotebookID)
		}
		if err == nil && nb != nil && nb.ServerNotebookID != "" {
			noteCopy.NotebookID = nb.ServerNotebookID
		}
	}

	user, _ := s.db.GetActiveUser()
	if user != nil && user.Host != "" && note.Content != "" {
		localPrefix := "/api2/file/getImage"
		noteCopy.Content = utils.FixNoteContentForSend(note.Content, user.Host, localPrefix)
	}

	files, fileDatas := s.getNoteFilesForUpload(note)
	if len(files) > 0 {
		noteCopy.Files = files
		noteCopy.FileDatas = fileDatas
	}

	return &noteCopy
}

func (s *SyncService) prepareNotebookForUpload(nb *models.Notebook) *models.Notebook {
	if nb == nil {
		return nil
	}
	nbCopy := *nb
	if nb.ParentNotebookID != "" {
		if parent, err := s.db.GetNotebook(nb.ParentNotebookID); err == nil && parent != nil && parent.ServerNotebookID != "" {
			nbCopy.ParentNotebookID = parent.ServerNotebookID
		}
	}
	return &nbCopy
}

func (s *SyncService) getNoteFilesForUpload(note *models.Note) ([]*models.FileRef, map[string]interface{}) {
	var files []*models.FileRef
	fileDatas := make(map[string]interface{})

	if note.Content == "" {
		return files, fileDatas
	}

	matches := localImageRe.FindAllStringSubmatch(note.Content, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		fileID := match[1]

		img, err := s.db.GetImage(fileID)
		if err != nil || img == nil {
			continue
		}

		if img.Path == "" || !img.IsDirty {
			continue
		}

		data, err := os.ReadFile(img.Path)
		if err != nil {
			logrus.Errorf("Read image file %s error: %v", img.Path, err)
			continue
		}

		ext := filepath.Ext(img.Path)
		if len(ext) > 0 {
			ext = ext[1:]
		}

		fileRef := &models.FileRef{
			FileID:      fileID,
			LocalFileID: fileID,
			Type:        ext,
			HasBody:     true,
			IsAttach:    false,
			IsDirty:     true,
		}
		if img.ServerFileID != "" {
			fileRef.FileID = img.ServerFileID
		}
		files = append(files, fileRef)

		fileDatas[fileID] = base64.StdEncoding.EncodeToString(data)
	}

	attachs, err := s.db.GetAttachsByNote(note.NoteID)
	if err == nil {
		for _, att := range attachs {
			if !att.IsDirty || att.Path == "" {
				continue
			}

			data, err := os.ReadFile(att.Path)
			if err != nil {
				logrus.Errorf("Read attach file %s error: %v", att.Path, err)
				continue
			}

			fileRef := &models.FileRef{
				FileID:      att.FileID,
				LocalFileID: att.FileID,
				Type:        att.Type,
				Title:       att.Title,
				HasBody:     true,
				IsAttach:    true,
				IsDirty:     true,
			}
			if att.ServerFileID != "" {
				fileRef.FileID = att.ServerFileID
			}
			files = append(files, fileRef)

			fileDatas[att.FileID] = base64.StdEncoding.EncodeToString(data)
		}
	}

	return files, fileDatas
}

func (s *SyncService) processNoteAfterSync(localNote *models.Note, serverNote *models.Note, isAdd bool) {
	if !serverNote.IsStarPresent {
		serverNote.IsStar = localNote.IsStar
	}
	if serverNote.Files != nil {
		for _, f := range serverNote.Files {
			if f.IsAttach {
				s.db.UpdateAttachServerID(f.LocalFileID, f.FileID)
			} else {
				s.db.UpdateImageServerID(f.LocalFileID, f.FileID)
			}
		}
	}

	serverNote.ServerNoteID = serverNote.NoteID
	serverNote.NoteID = localNote.NoteID
	s.db.UpdateNoteAfterSync(serverNote, isAdd)

	if isAdd {
		s.db.CountNotes(localNote.NotebookID)
	}

	s.updateTagCounts(localNote.Tags)
}

func (s *SyncService) updateTagCounts(tags []string) {
	userID := s.db.GetCurrentUserID()
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		count, err := s.db.CountNotesByTag(userID, tag)
		if err == nil {
			s.db.UpdateTagCount(tag, count)
		}
	}
}

func (s *SyncService) sendTagChanges(userID string, syncInfo *models.SyncInfo) error {
	tags, err := s.db.GetDirtyTags(userID)
	if err != nil {
		return err
	}

	var firstErr error
	for _, tag := range tags {
		if !tag.IsDirty {
			continue
		}

		if !tag.LocalIsDelete {
			serverTag, apiErr := s.api.AddTag(tag.Tag)
			if apiErr != nil {
				logrus.Errorf("Add tag error: %v", apiErr)
				if firstErr == nil {
					firstErr = apiErr
				}
				continue
			}

			if serverTag != nil {
				s.db.SetTagNotDirtyAndUsn(tag.Tag, serverTag.Usn)
				syncInfo.Tag.ChangeAdds = append(syncInfo.Tag.ChangeAdds, tag.Tag)
				s.checkNeedSyncAgain(serverTag.Usn)
			}
		} else {
			resp, apiErr := s.api.DeleteTag(tag)
			if apiErr != nil {
				logrus.Errorf("Delete tag error: %v", apiErr)
				if firstErr == nil {
					firstErr = apiErr
				}
				continue
			}

			if resp.Ok {
				s.db.SetTagNotDirty(tag.Tag)
				s.checkNeedSyncAgain(resp.Usn)
			} else if resp.Msg == "notExists" {
				s.db.DeleteLocalTag(tag.Tag)
			}
		}
	}

	return firstErr
}
