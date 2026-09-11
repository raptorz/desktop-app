package db

import (
	"database/sql"
	"time"

	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

func (d *Database) InsertImage(img *models.Image) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO images (_id, file_id, server_file_id, user_id, path, is_dirty, created_time)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, img.ID, img.FileID, img.ServerFileID, img.UserID, img.Path, img.IsDirty, utils.TimeToUnix(img.CreatedTime))
	return err
}

func (d *Database) GetImage(fileID string) (*models.Image, error) {
	row := d.db.QueryRow(`
		SELECT _id, file_id, server_file_id, user_id, path, is_dirty, created_time
		FROM images WHERE file_id = ?
	`, fileID)

	var img models.Image
	var createdTime sql.NullInt64

	err := row.Scan(&img.ID, &img.FileID, &img.ServerFileID, &img.UserID, &img.Path, &img.IsDirty, &createdTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		img.CreatedTime = &t
	}

	return &img, nil
}

func (d *Database) GetImageByServerID(serverFileID string) (*models.Image, error) {
	row := d.db.QueryRow(`
		SELECT _id, file_id, server_file_id, user_id, path, is_dirty, created_time
		FROM images WHERE server_file_id = ?
	`, serverFileID)

	var img models.Image
	var createdTime sql.NullInt64

	err := row.Scan(&img.ID, &img.FileID, &img.ServerFileID, &img.UserID, &img.Path, &img.IsDirty, &createdTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		img.CreatedTime = &t
	}

	return &img, nil
}

func (d *Database) UpdateImageServerID(fileID, serverFileID string) error {
	_, err := d.db.Exec(`UPDATE images SET server_file_id = ?, is_dirty = 0 WHERE file_id = ?`, serverFileID, fileID)
	return err
}

func (d *Database) GetDirtyImages(userID string) ([]*models.Image, error) {
	rows, err := d.db.Query(`
		SELECT _id, file_id, server_file_id, user_id, path, is_dirty, created_time
		FROM images WHERE user_id = ? AND is_dirty = 1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []*models.Image
	for rows.Next() {
		var img models.Image
		var createdTime sql.NullInt64

		if err := rows.Scan(&img.ID, &img.FileID, &img.ServerFileID, &img.UserID, &img.Path, &img.IsDirty, &createdTime); err != nil {
			return nil, err
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			img.CreatedTime = &t
		}
		images = append(images, &img)
	}
	return images, nil
}

func (d *Database) DeleteImage(fileID string) error {
	_, err := d.db.Exec(`DELETE FROM images WHERE file_id = ?`, fileID)
	return err
}

func (d *Database) InsertAttach(attach *models.Attach) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO attachs (_id, file_id, server_file_id, note_id, user_id, title, type, path, is_attach, is_dirty, created_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, attach.ID, attach.FileID, attach.ServerFileID, attach.NoteID, attach.UserID, attach.Title, attach.Type, attach.Path, attach.IsAttach, attach.IsDirty, utils.TimeToUnix(attach.CreatedTime))
	return err
}

func (d *Database) GetAttach(fileID string) (*models.Attach, error) {
	row := d.db.QueryRow(`
		SELECT _id, file_id, server_file_id, note_id, user_id, title, type, path, is_attach, is_dirty, created_time
		FROM attachs WHERE file_id = ?
	`, fileID)

	var attach models.Attach
	var createdTime sql.NullInt64

	err := row.Scan(&attach.ID, &attach.FileID, &attach.ServerFileID, &attach.NoteID, &attach.UserID, &attach.Title, &attach.Type, &attach.Path, &attach.IsAttach, &attach.IsDirty, &createdTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		attach.CreatedTime = &t
	}

	return &attach, nil
}

func (d *Database) GetAttachByServerID(serverFileID string) (*models.Attach, error) {
	row := d.db.QueryRow(`
		SELECT _id, file_id, server_file_id, note_id, user_id, title, type, path, is_attach, is_dirty, created_time
		FROM attachs WHERE server_file_id = ?
	`, serverFileID)

	var attach models.Attach
	var createdTime sql.NullInt64

	err := row.Scan(&attach.ID, &attach.FileID, &attach.ServerFileID, &attach.NoteID, &attach.UserID, &attach.Title, &attach.Type, &attach.Path, &attach.IsAttach, &attach.IsDirty, &createdTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		attach.CreatedTime = &t
	}

	return &attach, nil
}

func (d *Database) GetAttachsByNote(noteID string) ([]*models.Attach, error) {
	rows, err := d.db.Query(`
		SELECT _id, file_id, server_file_id, note_id, user_id, title, type, path, is_attach, is_dirty, created_time
		FROM attachs WHERE note_id = ?
	`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachs []*models.Attach
	for rows.Next() {
		var attach models.Attach
		var createdTime sql.NullInt64

		if err := rows.Scan(&attach.ID, &attach.FileID, &attach.ServerFileID, &attach.NoteID, &attach.UserID, &attach.Title, &attach.Type, &attach.Path, &attach.IsAttach, &attach.IsDirty, &createdTime); err != nil {
			return nil, err
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			attach.CreatedTime = &t
		}
		attachs = append(attachs, &attach)
	}
	return attachs, nil
}

func (d *Database) GetDirtyAttachs(userID string) ([]*models.Attach, error) {
	rows, err := d.db.Query(`
		SELECT _id, file_id, server_file_id, note_id, user_id, title, type, path, is_attach, is_dirty, created_time
		FROM attachs WHERE user_id = ? AND is_dirty = 1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachs []*models.Attach
	for rows.Next() {
		var attach models.Attach
		var createdTime sql.NullInt64

		if err := rows.Scan(&attach.ID, &attach.FileID, &attach.ServerFileID, &attach.NoteID, &attach.UserID, &attach.Title, &attach.Type, &attach.Path, &attach.IsAttach, &attach.IsDirty, &createdTime); err != nil {
			return nil, err
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			attach.CreatedTime = &t
		}
		attachs = append(attachs, &attach)
	}
	return attachs, nil
}

func (d *Database) UpdateAttachServerID(fileID, serverFileID string) error {
	_, err := d.db.Exec(`UPDATE attachs SET server_file_id = ?, is_dirty = 0 WHERE file_id = ?`, serverFileID, fileID)
	return err
}

func (d *Database) DeleteAttach(fileID string) error {
	_, err := d.db.Exec(`DELETE FROM attachs WHERE file_id = ?`, fileID)
	return err
}

func (d *Database) DeleteAttachsByNote(noteID string) error {
	_, err := d.db.Exec(`DELETE FROM attachs WHERE note_id = ?`, noteID)
	return err
}

func (d *Database) InsertNoteHistory(history *models.NoteHistory) error {
	_, err := d.db.Exec(`
		INSERT INTO note_histories (note_id, content, updated_time)
		VALUES (?, ?, ?)
	`, history.NoteID, history.Content, utils.TimeToUnix(history.UpdatedTime))
	return err
}

func (d *Database) GetNoteHistories(noteID string) ([]*models.NoteHistory, error) {
	rows, err := d.db.Query(`
		SELECT _id, note_id, content, updated_time
		FROM note_histories WHERE note_id = ?
		ORDER BY updated_time DESC
		LIMIT 20
	`, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var histories []*models.NoteHistory
	for rows.Next() {
		var h models.NoteHistory
		var updatedTime sql.NullInt64

		if err := rows.Scan(&h.ID, &h.NoteID, &h.Content, &updatedTime); err != nil {
			return nil, err
		}
		if updatedTime.Valid {
			t := utils.UnixToTime(updatedTime.Int64)
			h.UpdatedTime = &t
		}
		histories = append(histories, &h)
	}
	return histories, nil
}

func (d *Database) DeleteNoteHistories(noteID string) error {
	_, err := d.db.Exec(`DELETE FROM note_histories WHERE note_id = ?`, noteID)
	return err
}

func (d *Database) GetConfig(key string) (string, error) {
	row := d.db.QueryRow(`SELECT value FROM config WHERE key = ?`, key)
	var value string
	err := row.Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (d *Database) SetConfig(key, value string) error {
	_, err := d.db.Exec(`INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)`, key, value)
	return err
}

func (d *Database) AddNoteHistory(noteID, content string) error {
	d.DeleteOldNoteHistories(noteID, 19)

	now := time.Now()
	history := &models.NoteHistory{
		NoteID:      noteID,
		Content:     content,
		UpdatedTime: &now,
	}
	return d.InsertNoteHistory(history)
}

func (d *Database) DeleteOldNoteHistories(noteID string, keepCount int) error {
	_, err := d.db.Exec(`
		DELETE FROM note_histories 
		WHERE note_id = ? AND _id NOT IN (
			SELECT _id FROM note_histories WHERE note_id = ? ORDER BY updated_time DESC LIMIT ?
		)
	`, noteID, noteID, keepCount)
	return err
}

func (d *Database) CopyNote(noteID, targetNotebookID string) (*models.Note, error) {
	original, err := d.GetNote(noteID)
	if err != nil || original == nil {
		return nil, err
	}

	now := time.Now()
	newNote := &models.Note{
		ID:          utils.ObjectId(),
		NoteID:      utils.ObjectId(),
		NotebookID:  targetNotebookID,
		UserID:      original.UserID,
		Title:       original.Title,
		Content:     original.Content,
		Desc:        original.Desc,
		Tags:        original.Tags,
		IsMarkdown:  original.IsMarkdown,
		IsDirty:     true,
		LocalIsNew:  true,
		CreatedTime: original.CreatedTime,
		UpdatedTime: &now,
	}

	if err := d.InsertNote(newNote); err != nil {
		return nil, err
	}

	return newNote, nil
}

func (d *Database) ClearTrash(userID string) error {
	_, err := d.db.Exec(`DELETE FROM notes WHERE user_id = ? AND is_trash = 1`, userID)
	return err
}

func (d *Database) GetAllUsers() ([]*models.User, error) {
	rows, err := d.db.Query(`
		SELECT _id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		FROM users ORDER BY last_login_time DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		var lastSyncTime, createdTime, lastLoginTime sql.NullInt64

		if err := rows.Scan(
			&user.ID, &user.Username, &user.Email, &user.Pwd, &user.Token, &user.Host,
			&user.LastSyncUsn, &lastSyncTime, &user.NotebookUsn, &user.NoteUsn, &user.TagUsn,
			&user.IsActive, &user.IsLocal, &user.HasDB, &user.State, &createdTime, &lastLoginTime,
		); err != nil {
			return nil, err
		}

		if lastSyncTime.Valid {
			t := utils.UnixToTime(lastSyncTime.Int64)
			user.LastSyncTime = &t
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			user.CreatedTime = &t
		}
		if lastLoginTime.Valid {
			t := utils.UnixToTime(lastLoginTime.Int64)
			user.LastLoginTime = &t
		}

		users = append(users, &user)
	}
	return users, nil
}

func (d *Database) GetUser(userID string) (*models.User, error) {
	row := d.db.QueryRow(`
		SELECT _id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		FROM users WHERE _id = ?
	`, userID)

	var user models.User
	var lastSyncTime, createdTime, lastLoginTime sql.NullInt64

	err := row.Scan(
		&user.ID, &user.Username, &user.Email, &user.Pwd, &user.Token, &user.Host,
		&user.LastSyncUsn, &lastSyncTime, &user.NotebookUsn, &user.NoteUsn, &user.TagUsn,
		&user.IsActive, &user.IsLocal, &user.HasDB, &user.State, &createdTime, &lastLoginTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if lastSyncTime.Valid {
		t := utils.UnixToTime(lastSyncTime.Int64)
		user.LastSyncTime = &t
	}
	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		user.CreatedTime = &t
	}
	if lastLoginTime.Valid {
		t := utils.UnixToTime(lastLoginTime.Int64)
		user.LastLoginTime = &t
	}

	return &user, nil
}

func (d *Database) GetUserByNameOrEmail(identifier string) (*models.User, error) {
	row := d.db.QueryRow(`
		SELECT _id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		FROM users WHERE username = ? OR email = ?
	`, identifier, identifier)

	var user models.User
	var lastSyncTime, createdTime, lastLoginTime sql.NullInt64

	err := row.Scan(
		&user.ID, &user.Username, &user.Email, &user.Pwd, &user.Token, &user.Host,
		&user.LastSyncUsn, &lastSyncTime, &user.NotebookUsn, &user.NoteUsn, &user.TagUsn,
		&user.IsActive, &user.IsLocal, &user.HasDB, &user.State, &createdTime, &lastLoginTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if lastSyncTime.Valid {
		t := utils.UnixToTime(lastSyncTime.Int64)
		user.LastSyncTime = &t
	}
	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		user.CreatedTime = &t
	}
	if lastLoginTime.Valid {
		t := utils.UnixToTime(lastLoginTime.Int64)
		user.LastLoginTime = &t
	}

	return &user, nil
}

func (d *Database) DeleteUser(userID string) error {
	_, err := d.db.Exec(`DELETE FROM users WHERE _id = ?`, userID)
	return err
}

func (d *Database) DeactivateAllUsers() error {
	_, err := d.db.Exec(`UPDATE users SET is_active = 0`)
	return err
}

func (d *Database) SwitchUser(userID string) error {
	d.db.Exec(`UPDATE users SET is_active = 0`)
	_, err := d.db.Exec(`UPDATE users SET is_active = 1 WHERE _id = ?`, userID)
	return err
}
