package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

func (d *Database) InsertNote(note *models.Note) error {
	tagsJSON, _ := json.Marshal(note.Tags)

	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO notes (
			_id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		note.ID, note.NoteID, note.ServerNoteID, note.NotebookID, note.UserID,
		note.Title, note.Content, note.Desc, note.Abstract, note.ImgSrc, string(tagsJSON),
		note.IsMarkdown, note.IsTrash, note.IsBlog, note.IsStar, note.Usn,
		note.IsDirty, note.ContentIsDirty, note.LocalIsNew, note.LocalIsDelete, note.InitSync,
		note.ConflictNoteID, utils.TimeToUnix(note.ConflictTime), note.ConflictFixed, note.Err,
		utils.TimeToUnix(note.CreatedTime), utils.TimeToUnix(note.UpdatedTime),
	)
	return err
}

func (d *Database) InsertNoteTx(tx *sql.Tx, note *models.Note) error {
	tagsJSON, _ := json.Marshal(note.Tags)

	_, err := tx.Exec(`
		INSERT OR REPLACE INTO notes (
			_id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		note.ID, note.NoteID, note.ServerNoteID, note.NotebookID, note.UserID,
		note.Title, note.Content, note.Desc, note.Abstract, note.ImgSrc, string(tagsJSON),
		note.IsMarkdown, note.IsTrash, note.IsBlog, note.IsStar, note.Usn,
		note.IsDirty, note.ContentIsDirty, note.LocalIsNew, note.LocalIsDelete, note.InitSync,
		note.ConflictNoteID, utils.TimeToUnix(note.ConflictTime), note.ConflictFixed, note.Err,
		utils.TimeToUnix(note.CreatedTime), utils.TimeToUnix(note.UpdatedTime),
	)
	return err
}

func (d *Database) GetNote(noteID string) (*models.Note, error) {
	row := d.db.QueryRow(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes WHERE note_id = ?
	`, noteID)

	return d.scanNote(row)
}

func (d *Database) GetNoteByServerID(serverNoteID string) (*models.Note, error) {
	row := d.db.QueryRow(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes WHERE server_note_id = ?
	`, serverNoteID)

	return d.scanNote(row)
}

func (d *Database) GetLocalNoteID(serverNoteID string) (string, error) {
	row := d.db.QueryRow(`SELECT note_id FROM notes WHERE server_note_id = ?`, serverNoteID)
	var noteID string
	err := row.Scan(&noteID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return noteID, err
}

func (d *Database) GetServerNoteID(noteID string) (string, error) {
	row := d.db.QueryRow(`SELECT server_note_id FROM notes WHERE note_id = ?`, noteID)
	var serverID string
	err := row.Scan(&serverID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return serverID, err
}

func (d *Database) GetNotes(notebookID string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes 
		WHERE notebook_id = ? AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
		ORDER BY updated_time DESC
	`, notebookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) GetTrashNotes(userID string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes 
		WHERE user_id = ? AND is_trash = 1 AND (local_is_delete = 0 OR local_is_delete IS NULL)
		ORDER BY updated_time DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) GetAllNotes(userID string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes
		WHERE user_id = ? AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
		ORDER BY updated_time DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) MarkNoteLocalDelete(noteID string) error {
	_, err := d.db.Exec(`
		UPDATE notes SET local_is_delete = 1, is_dirty = 1, is_trash = 1, updated_time = ?
		WHERE note_id = ?
	`, time.Now().Unix(), noteID)
	return err
}

func (d *Database) GetStarNotes(userID string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes
		WHERE user_id = ? AND is_star = 1 AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
		ORDER BY updated_time DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) SearchNotes(userID, keyword string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes 
		WHERE user_id = ? AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
			AND (title LIKE ? OR content LIKE ?)
		ORDER BY updated_time DESC
	`, userID, "%"+keyword+"%", "%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) SearchNotesByTag(userID, tag string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes 
		WHERE user_id = ? AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
			AND tags LIKE ?
		ORDER BY updated_time DESC
	`, userID, "%\""+tag+"\"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) GetDirtyNotes(userID string) ([]*models.Note, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, server_note_id, notebook_id, user_id,
			title, content, desc, abstract, img_src, tags,
			is_markdown, is_trash, is_blog, is_star, usn,
			is_dirty, content_is_dirty, local_is_new, local_is_delete, init_sync,
			conflict_note_id, conflict_time, conflict_fixed, err,
			created_time, updated_time
		FROM notes WHERE user_id = ? AND is_dirty = 1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotes(rows)
}

func (d *Database) UpdateNote(note *models.Note) error {
	tagsJSON, _ := json.Marshal(note.Tags)

	_, err := d.db.Exec(`
		UPDATE notes SET
			title = ?, content = ?, desc = ?, tags = ?, 
			is_dirty = ?, content_is_dirty = ?, updated_time = ?
		WHERE note_id = ?
	`, note.Title, note.Content, note.Desc, string(tagsJSON), note.IsDirty, note.ContentIsDirty, time.Now().Unix(), note.NoteID)
	return err
}

func (d *Database) UpdateNoteForce(note *models.Note, needReloadContent bool) error {
	tagsJSON, _ := json.Marshal(note.Tags)

	initSync := note.InitSync
	if !needReloadContent {
		initSync = false
	}

	_, err := d.db.Exec(`
		UPDATE notes SET
			title = ?, desc = ?, abstract = ?, img_src = ?, tags = ?,
			is_markdown = ?, is_trash = ?, is_blog = ?, is_star = ?, usn = ?,
			is_dirty = 0, content_is_dirty = 0, local_is_new = 0, local_is_delete = 0, init_sync = ?,
			err = ''
		WHERE note_id = ?
	`, note.Title, note.Desc, note.Abstract, note.ImgSrc, string(tagsJSON),
		note.IsMarkdown, note.IsTrash, note.IsBlog, note.IsStar, note.Usn, initSync, note.NoteID)
	return err
}

func (d *Database) AddNoteForce(note *models.Note) (*models.Note, error) {
	if note.ID == "" {
		note.ID = utils.ObjectId()
	}
	if note.NoteID == "" {
		note.NoteID = note.ID
	}
	if note.ServerNoteID == "" && note.NoteID != "" {
		note.ServerNoteID = note.NoteID
	}

	localNotebookID, _ := d.GetNotebookIDByServerID(note.NotebookID)
	if localNotebookID != "" {
		note.NotebookID = localNotebookID
	}

	note.IsDirty = false
	note.LocalIsNew = false
	note.LocalIsDelete = false
	note.InitSync = true

	err := d.InsertNote(note)
	if err != nil {
		return nil, err
	}
	return note, nil
}

func (d *Database) DeleteNote(noteID string) error {
	_, err := d.db.Exec(`
		UPDATE notes SET is_trash = 1, is_dirty = 1, updated_time = ?
		WHERE note_id = ?
	`, time.Now().Unix(), noteID)
	return err
}

func (d *Database) DeleteNoteForce(noteID string) error {
	_, err := d.db.Exec(`DELETE FROM notes WHERE server_note_id = ?`, noteID)
	return err
}

func (d *Database) DeleteLocalNote(noteID string) error {
	_, err := d.db.Exec(`DELETE FROM notes WHERE note_id = ?`, noteID)
	return err
}

func (d *Database) SetNoteTrash(noteID string, isTrash bool) error {
	val := 0
	if isTrash {
		val = 1
	}
	_, err := d.db.Exec(`
		UPDATE notes SET is_trash = ?, is_dirty = 1, updated_time = ?
		WHERE note_id = ?
	`, val, time.Now().Unix(), noteID)
	return err
}

func (d *Database) MoveNote(noteID, notebookID string) error {
	_, err := d.db.Exec(`
		UPDATE notes SET notebook_id = ?, is_dirty = 1, is_trash = 0, local_is_delete = 0, updated_time = ?
		WHERE note_id = ?
	`, notebookID, time.Now().Unix(), noteID)
	return err
}

func (d *Database) StarNote(noteID string) error {
	_, err := d.db.Exec(`
		UPDATE notes SET is_star = CASE WHEN is_star = 1 THEN 0 ELSE 1 END, is_dirty = 1, updated_time = ?
		WHERE note_id = ?
	`, time.Now().Unix(), noteID)
	return err
}

func (d *Database) SetStar(noteID string, starred bool) error {
	value := 0
	if starred {
		value = 1
	}
	_, err := d.db.Exec(`UPDATE notes SET is_star = ?, is_dirty = 1, updated_time = ? WHERE note_id = ?`, value, time.Now().Unix(), noteID)
	return err
}

func (d *Database) HasNotes(notebookID string) (bool, error) {
	row := d.db.QueryRow(`
		SELECT COUNT(*) FROM notes 
		WHERE notebook_id = ? AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
	`, notebookID)
	var count int
	err := row.Scan(&count)
	return count > 0, err
}

func (d *Database) CountNotes(notebookID string) (int, error) {
	row := d.db.QueryRow(`
		SELECT COUNT(*) FROM notes 
		WHERE notebook_id = ? AND is_trash = 0 AND (local_is_delete = 0 OR local_is_delete IS NULL)
	`, notebookID)
	var count int
	err := row.Scan(&count)
	return count, err
}

func (d *Database) CountStarredNotes(userID string) (int, error) {
	row := d.db.QueryRow(`
		SELECT COUNT(*) FROM notes
		WHERE user_id = ? AND is_star = 1 AND is_trash = 0
		  AND (local_is_delete = 0 OR local_is_delete IS NULL)
	`, userID)
	var count int
	err := row.Scan(&count)
	return count, err
}

func (d *Database) UpdateNoteContent(noteID, content string) error {
	_, err := d.db.Exec(`
		UPDATE notes SET content = ?, init_sync = 0, content_is_dirty = 0, err = ''
		WHERE note_id = ?
	`, content, noteID)
	return err
}

func (d *Database) UpdateNoteUsn(noteID string, usn int64) error {
	_, err := d.db.Exec(`UPDATE notes SET usn = ?, is_dirty = 1 WHERE note_id = ?`, usn, noteID)
	return err
}

func (d *Database) SetNoteNotDirty(noteID string) error {
	_, err := d.db.Exec(`UPDATE notes SET is_dirty = 0 WHERE note_id = ?`, noteID)
	return err
}

func (d *Database) SetNoteError(noteID, errMsg string) error {
	_, err := d.db.Exec(`UPDATE notes SET err = ? WHERE note_id = ?`, errMsg, noteID)
	return err
}

func (d *Database) UpdateNoteAfterSync(note *models.Note, isAdd bool) error {
	tagsJSON, _ := json.Marshal(note.Tags)

	_, err := d.db.Exec(`
		UPDATE notes SET
			server_note_id = ?, usn = ?, title = ?, tags = ?, is_star = ?,
			is_dirty = 0, local_is_new = 0, content_is_dirty = 0, init_sync = 0, err = ''
		WHERE note_id = ?
	`, note.ServerNoteID, note.Usn, note.Title, string(tagsJSON), note.IsStar, note.NoteID)
	return err
}

func (d *Database) scanNote(row *sql.Row) (*models.Note, error) {
	var note models.Note
	var tagsJSON string
	var createdTime, updatedTime, conflictTime sql.NullInt64

	err := row.Scan(
		&note.ID, &note.NoteID, &note.ServerNoteID, &note.NotebookID, &note.UserID,
		&note.Title, &note.Content, &note.Desc, &note.Abstract, &note.ImgSrc, &tagsJSON,
		&note.IsMarkdown, &note.IsTrash, &note.IsBlog, &note.IsStar, &note.Usn,
		&note.IsDirty, &note.ContentIsDirty, &note.LocalIsNew, &note.LocalIsDelete, &note.InitSync,
		&note.ConflictNoteID, &conflictTime, &note.ConflictFixed, &note.Err,
		&createdTime, &updatedTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if tagsJSON != "" {
		json.Unmarshal([]byte(tagsJSON), &note.Tags)
	}
	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		note.CreatedTime = &t
	}
	if updatedTime.Valid {
		t := utils.UnixToTime(updatedTime.Int64)
		note.UpdatedTime = &t
	}
	if conflictTime.Valid {
		t := utils.UnixToTime(conflictTime.Int64)
		note.ConflictTime = &t
	}

	return &note, nil
}

func (d *Database) scanNotes(rows *sql.Rows) ([]*models.Note, error) {
	var notes []*models.Note
	for rows.Next() {
		var note models.Note
		var tagsJSON string
		var createdTime, updatedTime, conflictTime sql.NullInt64

		err := rows.Scan(
			&note.ID, &note.NoteID, &note.ServerNoteID, &note.NotebookID, &note.UserID,
			&note.Title, &note.Content, &note.Desc, &note.Abstract, &note.ImgSrc, &tagsJSON,
			&note.IsMarkdown, &note.IsTrash, &note.IsBlog, &note.IsStar, &note.Usn,
			&note.IsDirty, &note.ContentIsDirty, &note.LocalIsNew, &note.LocalIsDelete, &note.InitSync,
			&note.ConflictNoteID, &conflictTime, &note.ConflictFixed, &note.Err,
			&createdTime, &updatedTime,
		)
		if err != nil {
			return nil, err
		}

		if tagsJSON != "" {
			json.Unmarshal([]byte(tagsJSON), &note.Tags)
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			note.CreatedTime = &t
		}
		if updatedTime.Valid {
			t := utils.UnixToTime(updatedTime.Int64)
			note.UpdatedTime = &t
		}
		if conflictTime.Valid {
			t := utils.UnixToTime(conflictTime.Int64)
			note.ConflictTime = &t
		}

		notes = append(notes, &note)
	}
	return notes, nil
}

func (d *Database) CopyNoteForConflict(noteID string) (*models.Note, error) {
	original, err := d.GetNote(noteID)
	if err != nil || original == nil {
		return nil, err
	}

	newNote := &models.Note{
		ID:             utils.ObjectId(),
		NoteID:         utils.ObjectId(),
		NotebookID:     original.NotebookID,
		UserID:         original.UserID,
		Title:          original.Title,
		Content:        original.Content,
		Tags:           original.Tags,
		IsMarkdown:     original.IsMarkdown,
		IsDirty:        true,
		LocalIsNew:     true,
		ConflictNoteID: noteID,
		ConflictFixed:  false,
		InitSync:       false,
		CreatedTime:    original.CreatedTime,
		UpdatedTime:    original.UpdatedTime,
	}

	t := time.Now()
	newNote.ConflictTime = &t

	err = d.InsertNote(newNote)
	if err != nil {
		return nil, err
	}

	return newNote, nil
}

func (d *Database) CountNotesByTag(userID, tagName string) (int, error) {
	rows, err := d.db.Query(`
		SELECT COUNT(*) FROM notes 
		WHERE user_id = ? AND is_trash = 0 AND tags LIKE ?
	`, userID, "%\""+tagName+"\"%")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}
	return count, nil
}

func (d *Database) GetNoteFiles(noteID string) ([]*models.FileRef, error) {
	var files []*models.FileRef

	attachs, err := d.GetAttachsByNote(noteID)
	if err == nil {
		for _, att := range attachs {
			files = append(files, &models.FileRef{
				FileID:       att.FileID,
				ServerFileID: att.ServerFileID,
				Type:         att.Type,
				Title:        att.Title,
				Path:         att.Path,
				IsAttach:     true,
				HasBody:      true,
				IsDirty:      att.IsDirty,
			})
		}
	}

	return files, nil
}
