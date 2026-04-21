package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"leanote/models"
	"leanote/utils"

	_ "modernc.org/sqlite"
)

//go:embed migrations.sql
var migrationsFS embed.FS

type Database struct {
	db     *sql.DB
	dbPath string
	mu     sync.RWMutex

	currentUserID string
}

func New(dbPath string) (*Database, error) {
	if dbPath == "" {
		homeDir, _ := os.UserHomeDir()
		dbPath = filepath.Join(homeDir, ".leanote", "leanote.db")
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	database := &Database{
		db:     db,
		dbPath: dbPath,
	}

	if err := database.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	logrus.Info("Database initialized: ", dbPath)
	return database, nil
}

func NewInMemory() (*Database, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}

	database := &Database{
		db: db,
	}

	if err := database.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return database, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) migrate() error {
	migrationSQL, err := migrationsFS.ReadFile("migrations.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	_, err = d.db.Exec(string(migrationSQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	logrus.Info("Database migration completed")
	return nil
}

func (d *Database) SetCurrentUser(userID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentUserID = userID
}

func (d *Database) GetCurrentUserID() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentUserID
}

func (d *Database) GetDBPath() string {
	return d.dbPath
}

func (d *Database) Begin() (*sql.Tx, error) {
	return d.db.Begin()
}

// ==================== User Operations ====================

func (d *Database) InsertUser(user *models.User) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO users (
			_id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		user.ID, user.Username, user.Email, user.Pwd, user.Token, user.Host,
		user.LastSyncUsn, utils.TimeToUnix(user.LastSyncTime), user.NotebookUsn, user.NoteUsn, user.TagUsn,
		user.IsActive, user.IsLocal, user.HasDB, user.State, utils.TimeToUnix(user.CreatedTime), utils.TimeToUnix(user.LastLoginTime),
	)
	return err
}

func (d *Database) GetActiveUser() (*models.User, error) {
	row := d.db.QueryRow(`
		SELECT _id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		FROM users WHERE is_active = 1 LIMIT 1
	`)

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

func (d *Database) UpdateUserToken(userID, token string) error {
	_, err := d.db.Exec(`UPDATE users SET token = ? WHERE _id = ?`, token, userID)
	return err
}

func (d *Database) UpdateLastSyncUsn(userID string, usn int64) error {
	_, err := d.db.Exec(`UPDATE users SET last_sync_usn = ? WHERE _id = ?`, usn, userID)
	return err
}

func (d *Database) GetAllLastSyncState(userID string) (lastUsn, notebookUsn, noteUsn, tagUsn int64, err error) {
	row := d.db.QueryRow(`
		SELECT last_sync_usn, notebook_usn, note_usn, tag_usn
		FROM users WHERE _id = ?
	`, userID)

	err = row.Scan(&lastUsn, &notebookUsn, &noteUsn, &tagUsn)
	return
}

func (d *Database) UpdateUserSyncState(userID string, state map[string]int64) error {
	_, err := d.db.Exec(`
		UPDATE users SET 
			last_sync_usn = ?, 
			notebook_usn = ?, 
			note_usn = ?, 
			tag_usn = ?,
			last_sync_time = ?
		WHERE _id = ?
	`, state["last_sync_usn"], state["notebook_usn"], state["note_usn"], state["tag_usn"],
		state["last_sync_time"], userID)
	return err
}

func (d *Database) SetUserHasDB(userID string, hasDB bool) error {
	val := 0
	if hasDB {
		val = 1
	}
	_, err := d.db.Exec(`UPDATE users SET has_db = ? WHERE _id = ?`, val, userID)
	return err
}

func (d *Database) UpdateLastLoginTime(userID string) error {
	_, err := d.db.Exec(`UPDATE users SET last_login_time = ? WHERE _id = ?`, time.Now().Unix(), userID)
	return err
}

func (d *Database) GetUserByEmail(email string) (*models.User, error) {
	row := d.db.QueryRow(`
		SELECT _id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		FROM users WHERE email = ?
	`, email)

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

func (d *Database) UpdateUserHost(userID, host string) error {
	_, err := d.db.Exec(`UPDATE users SET host = ? WHERE _id = ?`, host, userID)
	return err
}

func (d *Database) UpdateUserPwd(userID, pwd string) error {
	_, err := d.db.Exec(`UPDATE users SET pwd = ? WHERE _id = ?`, pwd, userID)
	return err
}

func (d *Database) GetUserByUsername(username string) (*models.User, error) {
	row := d.db.QueryRow(`
		SELECT _id, username, email, pwd, token, host,
			last_sync_usn, last_sync_time, notebook_usn, note_usn, tag_usn,
			is_active, is_local, has_db, state, created_time, last_login_time
		FROM users WHERE username = ? AND is_local = 1
	`, username)

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

func (d *Database) CountNotebooks(userID string) (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM notebooks WHERE user_id = ? AND local_is_delete = 0`, userID).Scan(&count)
	return count, err
}

func (d *Database) CountAllNotes(userID string) (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE user_id = ? AND is_trash = 0 AND local_is_delete = 0`, userID).Scan(&count)
	return count, err
}

func (d *Database) CountTags(userID string) (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE user_id = ? AND local_is_delete = 0`, userID).Scan(&count)
	return count, err
}

// ==================== Missing Methods ====================

func (d *Database) UpdateUser(user *models.User) error {
	_, err := d.db.Exec(`
		UPDATE users SET username = ?, email = ?, token = ?, host = ?,
			is_active = ?, is_local = ?, last_sync_usn = ?
		WHERE _id = ?
	`, user.Username, user.Email, user.Token, user.Host, user.IsActive, user.IsLocal, user.LastSyncUsn, user.ID)
	return err
}

func (d *Database) MarkAllDataAsLocal(userID string) error {
	d.db.Exec(`UPDATE notes SET server_note_id = '', is_dirty = 1, local_is_new = 1 WHERE user_id = ?`, userID)
	d.db.Exec(`UPDATE notebooks SET server_notebook_id = '', is_dirty = 1, local_is_new = 1 WHERE user_id = ?`, userID)
	d.db.Exec(`UPDATE tags SET is_dirty = 1 WHERE user_id = ?`, userID)
	d.db.Exec(`UPDATE images SET server_file_id = '', is_dirty = 1 WHERE user_id = ?`, userID)
	d.db.Exec(`UPDATE attachs SET server_file_id = '', is_dirty = 1 WHERE user_id = ?`, userID)
	return nil
}

func (d *Database) DeleteAllNotes(userID string) error {
	_, err := d.db.Exec(`DELETE FROM notes WHERE user_id = ?`, userID)
	return err
}

func (d *Database) DeleteAllNotebooks(userID string) error {
	_, err := d.db.Exec(`DELETE FROM notebooks WHERE user_id = ?`, userID)
	return err
}

func (d *Database) DeleteAllTags(userID string) error {
	_, err := d.db.Exec(`DELETE FROM tags WHERE user_id = ?`, userID)
	return err
}

func (d *Database) DeleteAllImages(userID string) error {
	_, err := d.db.Exec(`DELETE FROM images WHERE user_id = ?`, userID)
	return err
}

func (d *Database) DeleteAllAttachs(userID string) error {
	_, err := d.db.Exec(`DELETE FROM attachs WHERE user_id = ?`, userID)
	return err
}

func (d *Database) DeleteAllNoteHistories(userID string) error {
	_, err := d.db.Exec(`DELETE FROM note_histories WHERE note_id IN (SELECT note_id FROM notes WHERE user_id = ?)`, userID)
	return err
}

func (d *Database) GetAllAttachs(userID string) ([]*models.Attach, error) {
	rows, err := d.db.Query(`
		SELECT _id, file_id, server_file_id, note_id, user_id, title, type, path, is_attach, is_dirty, created_time
		FROM attachs WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachs []*models.Attach
	for rows.Next() {
		var att models.Attach
		var createdTime sql.NullInt64
		err := rows.Scan(&att.ID, &att.FileID, &att.ServerFileID, &att.NoteID, &att.UserID,
			&att.Title, &att.Type, &att.Path, &att.IsAttach, &att.IsDirty, &createdTime)
		if err != nil {
			continue
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			att.CreatedTime = &t
		}
		attachs = append(attachs, &att)
	}
	return attachs, nil
}

func (d *Database) GetAllImages(userID string) ([]*models.Image, error) {
	rows, err := d.db.Query(`
		SELECT _id, file_id, server_file_id, user_id, path, is_dirty, created_time
		FROM images WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []*models.Image
	for rows.Next() {
		var img models.Image
		var createdTime sql.NullInt64
		err := rows.Scan(&img.ID, &img.FileID, &img.ServerFileID, &img.UserID, &img.Path, &img.IsDirty, &createdTime)
		if err != nil {
			continue
		}
		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			img.CreatedTime = &t
		}
		images = append(images, &img)
	}
	return images, nil
}
