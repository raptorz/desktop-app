package sync

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"

	"github.com/sirupsen/logrus"

	"leanote/api"
	"leanote/models"
	"leanote/utils"
)

var localImageRe = regexp.MustCompile(`leanote://file/getImage\?fileId=([a-zA-Z0-9]{24})`)

func (s *SyncService) sendChanges(syncInfo *models.SyncInfo) error {
	logrus.Info("Sending changes...")

	userID := s.db.GetCurrentUserID()

	if err := s.sendNotebookChanges(userID, syncInfo); err != nil {
		logrus.Errorf("Send notebook changes error: %v", err)
	}

	if err := s.sendNoteChanges(userID, syncInfo); err != nil {
		logrus.Errorf("Send note changes error: %v", err)
	}

	if err := s.sendTagChanges(userID, syncInfo); err != nil {
		logrus.Errorf("Send tag changes error: %v", err)
	}

	return nil
}

func (s *SyncService) sendNotebookChanges(userID string, syncInfo *models.SyncInfo) error {
	notebooks, err := s.db.GetDirtyNotebooks(userID)
	if err != nil {
		return err
	}

	for _, nb := range notebooks {
		if nb.LocalIsNew && nb.LocalIsDelete {
			s.db.DeleteLocalNotebook(nb.NotebookID)
			continue
		}

		var serverNb *models.Notebook
		var apiErr error

		if nb.LocalIsNew {
			serverNb, apiErr = s.api.AddNotebook(nb)
		} else if nb.LocalIsDelete {
			var resp *api.APIResponse
			resp, apiErr = s.api.DeleteNotebook(nb)
			if resp != nil && resp.Ok {
				s.db.SetNotebookNotDirty(nb.NotebookID)
			}
			continue
		} else {
			serverNb, apiErr = s.api.UpdateNotebook(nb)
		}

		if apiErr != nil {
			logrus.Errorf("API error for notebook %s: %v", nb.NotebookID, apiErr)
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

	return nil
}

func (s *SyncService) sendNoteChanges(userID string, syncInfo *models.SyncInfo) error {
	notes, err := s.db.GetDirtyNotes(userID)
	if err != nil {
		return err
	}

	for _, note := range notes {
		if note.InitSync {
			continue
		}

		if note.ConflictNoteID != "" && !note.ConflictFixed {
			logrus.Debugf("Skip unfixed conflict note: %s", note.NoteID)
			continue
		}

		if note.LocalIsNew {
			if !note.IsTrash && !note.LocalIsDelete {
				noteCopy := s.prepareNoteForUpload(note)
				serverNote, apiErr := s.api.AddNote(noteCopy)
				if apiErr != nil {
					logrus.Errorf("Add note error: %v", apiErr)
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
				logrus.Errorf("Update note error: %v", apiErr)
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

	return nil
}

func (s *SyncService) prepareNoteForUpload(note *models.Note) *models.Note {
	noteCopy := *note

	user, _ := s.db.GetActiveUser()
	if user != nil && user.Host != "" && note.Content != "" {
		localPrefix := "leanote://file/getImage"
		noteCopy.Content = utils.FixNoteContentForSend(note.Content, user.Host, localPrefix)
	}

	files, fileDatas := s.getNoteFilesForUpload(note)
	if len(files) > 0 {
		noteCopy.Files = files
		noteCopy.FileDatas = fileDatas
	}

	return &noteCopy
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
			files = append(files, fileRef)

			fileDatas[att.FileID] = base64.StdEncoding.EncodeToString(data)
		}
	}

	return files, fileDatas
}

func (s *SyncService) processNoteAfterSync(localNote *models.Note, serverNote *models.Note, isAdd bool) {
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

	for _, tag := range tags {
		if !tag.IsDirty {
			continue
		}

		if !tag.LocalIsDelete {
			serverTag, apiErr := s.api.AddTag(tag.Tag)
			if apiErr != nil {
				logrus.Errorf("Add tag error: %v", apiErr)
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

	return nil
}
