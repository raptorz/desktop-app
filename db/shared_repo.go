package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"pearlnote/models"
	"pearlnote/utils"
)

func SharedAccountID(host, userID string) string {
	host = strings.TrimRight(strings.ToLower(strings.TrimSpace(host)), "/")
	if u, err := url.Parse(host); err == nil {
		u.Fragment, u.RawQuery = "", ""
		host = strings.TrimRight(u.String(), "/")
	}
	sum := sha256.Sum256([]byte(host + "\x00" + userID))
	return hex.EncodeToString(sum[:12])
}

func (d *Database) EnsureSharedAccount(accountID, host, userID string, protocol int, state string) error {
	_, err := d.db.Exec(`INSERT INTO shared_accounts(account_id,server_url,remote_user_id,protocol_version,capability_state)
		VALUES(?,?,?,?,?) ON CONFLICT(account_id) DO UPDATE SET protocol_version=excluded.protocol_version,capability_state=excluded.capability_state`,
		accountID, strings.TrimRight(host, "/"), userID, protocol, state)
	return err
}

// PublishSharedSnapshot publishes only a complete, validated snapshot. All
// metadata, revocations and retry jobs become visible in one transaction.
func (d *Database) PublishSharedSnapshot(accountID string, items []models.SharedSnapshotItem, expected int) error {
	if expected >= 0 && len(items) != expected {
		return fmt.Errorf("shared snapshot count mismatch: got %d want %d", len(items), expected)
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var generation int64
	if err = tx.QueryRow(`SELECT generation+1 FROM shared_sync_state WHERE account_id=?`, accountID).Scan(&generation); err == sql.ErrNoRows {
		generation = 1
	} else if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, item := range items {
		switch item.Kind {
		case "notebook":
			if item.Notebook == nil || item.Notebook.NotebookID == "" {
				return fmt.Errorf("invalid shared notebook item")
			}
			key := "b:" + item.Notebook.NotebookID
			if seen[key] {
				return fmt.Errorf("duplicate shared item %s", key)
			}
			seen[key] = true
			n := item.Notebook
			_, err = tx.Exec(`INSERT INTO shared_notebooks(account_id,server_notebook_id,owner_user_id,parent_notebook_id,title,seq,perm,generation,revoked)
				VALUES(?,?,?,?,?,?,?,?,0) ON CONFLICT(account_id,server_notebook_id) DO UPDATE SET owner_user_id=excluded.owner_user_id,parent_notebook_id=excluded.parent_notebook_id,title=excluded.title,seq=excluded.seq,perm=excluded.perm,generation=excluded.generation,revoked=0`, accountID, n.NotebookID, n.OwnerUserID, n.ParentNotebookID, n.Title, n.Seq, n.Perm, generation)
		case "note":
			if item.Note == nil || item.Note.NoteID == "" {
				return fmt.Errorf("invalid shared note item")
			}
			key := "n:" + item.Note.NoteID
			if seen[key] {
				return fmt.Errorf("duplicate shared item %s", key)
			}
			seen[key] = true
			n := item.Note
			tags, _ := json.Marshal(n.Tags)
			_, err = tx.Exec(`INSERT INTO shared_notes(account_id,server_note_id,server_notebook_id,owner_user_id,title,description,tags,is_markdown,perm,metadata_version,target_content_version,cache_state,generation,revoked,created_time,updated_time)
				VALUES(?,?,?,?,?,?,?,?,?,?,?, 'pending',?,0,?,?) ON CONFLICT(account_id,server_note_id) DO UPDATE SET server_notebook_id=excluded.server_notebook_id,owner_user_id=excluded.owner_user_id,title=excluded.title,description=excluded.description,tags=excluded.tags,is_markdown=excluded.is_markdown,perm=excluded.perm,metadata_version=excluded.metadata_version,target_content_version=excluded.target_content_version,cache_state=CASE WHEN shared_notes.cached_content_version=excluded.target_content_version THEN 'ready' WHEN shared_notes.content IS NULL THEN 'pending' ELSE 'stale' END,generation=excluded.generation,revoked=0,created_time=excluded.created_time,updated_time=excluded.updated_time WHERE excluded.generation>shared_notes.generation`, accountID, n.NoteID, n.NotebookID, n.OwnerUserID, n.Title, n.Desc, string(tags), n.IsMarkdown, n.Perm, n.MetadataVersion, n.TargetContentVersion, generation, utils.TimeToUnix(n.CreatedTime), utils.TimeToUnix(n.UpdatedTime))
			if err == nil {
				_, err = tx.Exec(`INSERT INTO shared_download_jobs(account_id,resource_type,resource_id,target_version,status) SELECT ?, 'content', ?, ?, 'pending' WHERE NOT EXISTS (SELECT 1 FROM shared_notes WHERE account_id=? AND server_note_id=? AND cached_content_version=?) ON CONFLICT(account_id,resource_type,resource_id,target_version) DO UPDATE SET status=CASE WHEN shared_download_jobs.status='done' THEN 'done' ELSE 'pending' END`, accountID, n.NoteID, n.TargetContentVersion, accountID, n.NoteID, n.TargetContentVersion)
			}
		case "file":
			if item.File == nil || item.File.NoteID == "" || item.File.FileID == "" {
				return fmt.Errorf("invalid shared file item")
			}
			key := "f:" + item.File.NoteID + ":" + item.File.Kind + ":" + item.File.FileID
			if seen[key] {
				return fmt.Errorf("duplicate shared item %s", key)
			}
			seen[key] = true
			f := item.File
			initialState := "available"
			if f.Kind == "image" {
				initialState = "pending"
			}
			_, err = tx.Exec(`INSERT INTO shared_files(account_id,server_note_id,server_file_id,kind,title,size,target_version,cache_state,generation,revoked)
				VALUES(?,?,?,?,?,?,?,?,?,0) ON CONFLICT(account_id,server_note_id,server_file_id,kind) DO UPDATE SET title=excluded.title,size=excluded.size,target_version=excluded.target_version,cache_state=CASE WHEN shared_files.target_version=excluded.target_version AND shared_files.cache_state='ready' THEN 'ready' WHEN excluded.kind='image' THEN 'pending' ELSE 'available' END,generation=excluded.generation,revoked=0 WHERE excluded.generation>shared_files.generation`,
				accountID, f.NoteID, f.FileID, f.Kind, f.Title, f.Size, f.Version, initialState, generation)
		default:
			return fmt.Errorf("unknown shared item kind %q", item.Kind)
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE shared_notebooks SET revoked=1 WHERE account_id=? AND generation<>?`, accountID, generation); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE shared_notes SET revoked=1,cache_state='revoked' WHERE account_id=? AND generation<>?`, accountID, generation); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE shared_files SET revoked=1,cache_state='revoked' WHERE account_id=? AND (generation<>? OR server_note_id IN (SELECT server_note_id FROM shared_notes WHERE account_id=? AND revoked=1))`, accountID, generation, accountID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM shared_download_jobs WHERE account_id=? AND resource_id IN (SELECT server_note_id FROM shared_notes WHERE account_id=? AND revoked=1)`, accountID, accountID); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO shared_sync_state(account_id,generation,last_checked_at,error) VALUES(?,?,?,'') ON CONFLICT(account_id) DO UPDATE SET generation=excluded.generation,last_checked_at=excluded.last_checked_at,error=''`, accountID, generation, time.Now().Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) PendingSharedContentJobs(accountID string, limit int) ([][2]string, error) {
	rows, err := d.db.Query(`SELECT resource_id,target_version FROM shared_download_jobs WHERE account_id=? AND resource_type='content' AND status='pending' AND next_retry_at<=? ORDER BY retries LIMIT ?`, accountID, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var v [2]string
		if err := rows.Scan(&v[0], &v[1]); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *Database) CompleteSharedContent(accountID, noteID, target, version, digest, content string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	r, err := tx.Exec(`UPDATE shared_notes SET content=?,cached_content_version=?,digest=?,cache_state='ready',cached_at=? WHERE account_id=? AND server_note_id=? AND target_content_version=? AND revoked=0`, content, version, digest, now, accountID, noteID, target)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("shared note changed or revoked")
	}
	if _, err = tx.Exec(`UPDATE shared_download_jobs SET status='done',error='' WHERE account_id=? AND resource_type='content' AND resource_id=? AND target_version=?`, accountID, noteID, target); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) FailSharedContent(accountID, noteID, target, msg string) error {
	_, err := d.db.Exec(`UPDATE shared_download_jobs SET retries=retries+1,next_retry_at=?,error=?,status='pending' WHERE account_id=? AND resource_type='content' AND resource_id=? AND target_version=?`, time.Now().Add(time.Minute).Unix(), msg, accountID, noteID, target)
	return err
}

func (d *Database) PendingSharedFileJobs(accountID string, kinds []string, limit int) ([]*models.SharedFile, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
	args := []any{accountID}
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, time.Now().Unix(), limit)
	rows, err := d.db.Query(`SELECT server_note_id,server_file_id,kind,title,size,target_version FROM shared_files
		WHERE account_id=? AND kind IN (`+placeholders+`) AND revoked=0 AND cache_state IN ('pending','failed') AND next_retry_at<=?
		ORDER BY retries, next_retry_at LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SharedFile
	for rows.Next() {
		var f models.SharedFile
		if err := rows.Scan(&f.NoteID, &f.FileID, &f.Kind, &f.Title, &f.Size, &f.TargetVersion); err != nil {
			return nil, err
		}
		f.AccountID = accountID
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (d *Database) CompleteSharedFile(accountID string, f *models.SharedFile, localPath string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	r, err := tx.Exec(`UPDATE shared_files SET local_path=?,cache_state='ready',error='',cached_at=?,target_version=?
		WHERE account_id=? AND server_note_id=? AND server_file_id=? AND kind=? AND target_version=? AND revoked=0`,
		localPath, now, f.TargetVersion, accountID, f.NoteID, f.FileID, f.Kind, f.TargetVersion)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return fmt.Errorf("shared file changed or revoked")
	}
	return tx.Commit()
}

func (d *Database) FailSharedFile(accountID string, f *models.SharedFile, msg string) error {
	_, err := d.db.Exec(`UPDATE shared_files SET retries=retries+1,next_retry_at=?,error=?,cache_state='failed'
		WHERE account_id=? AND server_note_id=? AND server_file_id=? AND kind=? AND target_version=?`,
		time.Now().Add(time.Minute).Unix(), msg, accountID, f.NoteID, f.FileID, f.Kind, f.TargetVersion)
	return err
}

// GetSharedFilePath resolves a cached shared file only while both the file and
// its owning note are still valid for the account.
func (d *Database) GetSharedFilePath(accountID, fileID string, kinds ...string) (string, string, string, error) {
	if len(kinds) == 0 {
		kinds = []string{"image", "attachment"}
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(kinds)), ",")
	args := []any{accountID, fileID}
	for _, k := range kinds {
		args = append(args, k)
	}
	var path, title, kind string
	err := d.db.QueryRow(`SELECT f.local_path, COALESCE(f.title,''), f.kind FROM shared_files f
		JOIN shared_notes n ON n.account_id=f.account_id AND n.server_note_id=f.server_note_id
		WHERE f.account_id=? AND f.server_file_id=? AND f.kind IN (`+placeholders+`) AND f.revoked=0 AND f.cache_state='ready' AND n.revoked=0`, args...).Scan(&path, &title, &kind)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", err
	}
	if path != "" {
		if _, statErr := os.Stat(path); statErr != nil {
			return "", "", "", nil
		}
	}
	return path, title, kind, nil
}

func (d *Database) IsSharedFile(accountID, fileID string) bool {
	var one int
	return d.db.QueryRow(`SELECT 1 FROM shared_files WHERE account_id=? AND server_file_id=?`, accountID, fileID).Scan(&one) == nil
}

func (d *Database) GetSharedNote(accountID, noteID string) (*models.SharedNote, error) {
	row := d.db.QueryRow(`SELECT account_id,server_note_id,server_notebook_id,owner_user_id,COALESCE(title,''),COALESCE(content,''),COALESCE(description,''),tags,COALESCE(is_markdown,0),COALESCE(perm,0),COALESCE(metadata_version,''),COALESCE(target_content_version,''),COALESCE(cached_content_version,''),COALESCE(digest,''),COALESCE(cache_state,''),COALESCE(generation,0),COALESCE(revoked,0),created_time,updated_time,cached_at FROM shared_notes WHERE account_id=? AND server_note_id=? AND revoked=0`, accountID, noteID)
	var n models.SharedNote
	var tags string
	var created, updated, cached sql.NullInt64
	err := row.Scan(&n.AccountID, &n.NoteID, &n.NotebookID, &n.OwnerUserID, &n.Title, &n.Content, &n.Desc, &tags, &n.IsMarkdown, &n.Perm, &n.MetadataVersion, &n.TargetContentVersion, &n.CachedContentVersion, &n.Digest, &n.CacheState, &n.Generation, &n.Revoked, &created, &updated, &cached)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(tags), &n.Tags)
	if created.Valid {
		t := time.Unix(created.Int64, 0)
		n.CreatedTime = &t
	}
	if updated.Valid {
		t := time.Unix(updated.Int64, 0)
		n.UpdatedTime = &t
	}
	if cached.Valid {
		t := time.Unix(cached.Int64, 0)
		n.CachedAt = &t
	}
	return &n, nil
}

func (d *Database) ListSharedNotes(accountID, owner, notebook, key string) ([]*models.SharedNote, error) {
	q := `SELECT server_note_id FROM shared_notes WHERE account_id=? AND revoked=0`
	args := []any{accountID}
	if owner != "" {
		q += " AND owner_user_id=?"
		args = append(args, owner)
	}
	if notebook != "" {
		q += " AND server_notebook_id=?"
		args = append(args, notebook)
	}
	if key != "" {
		q += " AND (title LIKE ? OR description LIKE ?)"
		args = append(args, "%"+key+"%", "%"+key+"%")
	}
	q += " ORDER BY updated_time DESC"
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SharedNote
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		n, e := d.GetSharedNote(accountID, id)
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (d *Database) SharedNotebooks(accountID string) (map[string]any, error) {
	rows, err := d.db.Query(`SELECT server_notebook_id,owner_user_id,parent_notebook_id,title,seq,perm FROM shared_notebooks WHERE account_id=? AND revoked=0 ORDER BY owner_user_id,seq,title`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := map[string][]map[string]any{}
	for rows.Next() {
		var id, owner, parent, title string
		var seq, perm int
		if err := rows.Scan(&id, &owner, &parent, &title, &seq, &perm); err != nil {
			return nil, err
		}
		groups[owner] = append(groups[owner], map[string]any{"NotebookId": id, "UserId": owner, "ParentNotebookId": parent, "Title": title, "Seq": seq, "Perm": perm})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	ownerRows, err := d.db.Query(`SELECT DISTINCT owner_user_id FROM shared_notes WHERE account_id=? AND revoked=0`, accountID)
	if err != nil {
		return nil, err
	}
	defer ownerRows.Close()
	for ownerRows.Next() {
		var owner string
		if err := ownerRows.Scan(&owner); err != nil {
			return nil, err
		}
		if _, ok := groups[owner]; !ok {
			groups[owner] = []map[string]any{}
		}
	}
	if err := ownerRows.Err(); err != nil {
		return nil, err
	}
	owners := make([]string, 0, len(groups))
	for owner := range groups {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	out := map[string]any{}
	for _, owner := range owners {
		books := groups[owner]
		books = append([]map[string]any{{"NotebookId": "", "UserId": owner, "Title": "共享文章", "IsDefault": true, "Perm": 0}}, books...)
		out[owner] = books
	}
	return out, nil
}

func (d *Database) IsSharedNote(accountID, noteID string) bool {
	var one int
	return d.db.QueryRow(`SELECT 1 FROM shared_notes WHERE account_id=? AND server_note_id=?`, accountID, noteID).Scan(&one) == nil
}

func (d *Database) IsSharedNotebook(accountID, notebookID string) bool {
	var one int
	return d.db.QueryRow(`SELECT 1 FROM shared_notebooks WHERE account_id=? AND server_notebook_id=? AND revoked=0`, accountID, notebookID).Scan(&one) == nil
}

func (d *Database) BeginSharedSnapshotStaging(accountID, snapshotID string, total int) error {
	_, err := d.db.Exec(`INSERT INTO shared_sync_state(account_id,staging_snapshot_id,staging_token,staging_total)
		VALUES(?,?, '', ?) ON CONFLICT(account_id) DO UPDATE SET staging_snapshot_id=excluded.staging_snapshot_id,staging_token='',staging_total=excluded.staging_total`,
		accountID, snapshotID, total)
	return err
}

func (d *Database) SaveSharedSnapshotStagingPage(accountID, snapshotID, nextToken string, items []models.SharedSnapshotItem) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		key := item.Kind + ":" + sharedStagingItemID(item)
		payload, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO shared_snapshot_items(account_id,snapshot_id,item_key,kind,payload) VALUES(?,?,?,?,?)`,
			accountID, snapshotID, key, item.Kind, string(payload)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE shared_sync_state SET staging_token=? WHERE account_id=? AND staging_snapshot_id=?`, nextToken, accountID, snapshotID); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) ReadSharedSnapshotStaging(accountID, snapshotID string) ([]models.SharedSnapshotItem, error) {
	rows, err := d.db.Query(`SELECT payload FROM shared_snapshot_items WHERE account_id=? AND snapshot_id=? ORDER BY item_key`, accountID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SharedSnapshotItem
	for rows.Next() {
		var payload string
		var item models.SharedSnapshotItem
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *Database) ClearSharedSnapshotStaging(accountID, snapshotID string) error {
	_, err := d.db.Exec(`DELETE FROM shared_snapshot_items WHERE account_id=? AND snapshot_id=?`, accountID, snapshotID)
	return err
}

func (d *Database) SavePendingSharedGeneration(accountID string, generation int64) error {
	_, err := d.db.Exec(`UPDATE shared_sync_state SET pending_generation=? WHERE account_id=?`, generation, accountID)
	return err
}

func (d *Database) SetSharedProbeBackoff(accountID string, until int64) error {
	_, err := d.db.Exec(`INSERT INTO shared_sync_state(account_id,next_probe_at) VALUES(?,?)
		ON CONFLICT(account_id) DO UPDATE SET next_probe_at=excluded.next_probe_at`, accountID, until)
	return err
}

func (d *Database) SharedProbePaused(accountID string) bool {
	var paused int
	return d.db.QueryRow(`SELECT 1 FROM shared_sync_state s JOIN shared_accounts a ON a.account_id=s.account_id
		WHERE s.account_id=? AND a.capability_state='unsupported' AND s.next_probe_at>?`, accountID, time.Now().Unix()).Scan(&paused) == nil
}

func (d *Database) SharedCapabilityState(accountID string) string {
	var state string
	d.db.QueryRow(`SELECT capability_state FROM shared_accounts WHERE account_id=?`, accountID).Scan(&state)
	return state
}

func (d *Database) RevokeSharedNote(accountID, noteID string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE shared_notes SET revoked=1,cache_state='revoked',generation=(SELECT COALESCE(generation,0)+1 FROM shared_sync_state WHERE account_id=?) WHERE account_id=? AND server_note_id=?`, accountID, accountID, noteID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE shared_files SET revoked=1,cache_state='revoked' WHERE account_id=? AND server_note_id=?`, accountID, noteID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM shared_download_jobs WHERE account_id=? AND resource_type='content' AND resource_id=?`, accountID, noteID); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) QueueSharedAttachment(accountID, fileID string) (int64, error) {
	r, err := d.db.Exec(`UPDATE shared_files SET cache_state='pending',next_retry_at=0 WHERE account_id=? AND server_file_id=? AND kind='attachment' AND revoked=0`, accountID, fileID)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func (d *Database) ListSharedAttachmentsForNote(accountID, noteID string) ([]*models.SharedFile, error) {
	rows, err := d.db.Query(`SELECT server_file_id,kind,title,size,cache_state,cached_at FROM shared_files WHERE account_id=? AND server_note_id=? AND kind='attachment' AND revoked=0 ORDER BY title`, accountID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SharedFile
	for rows.Next() {
		var f models.SharedFile
		var cached sql.NullInt64
		if err := rows.Scan(&f.FileID, &f.Kind, &f.Title, &f.Size, &f.CacheState, &cached); err != nil {
			return nil, err
		}
		f.NoteID = noteID
		f.AccountID = accountID
		if cached.Valid {
			t := time.Unix(cached.Int64, 0)
			f.CachedAt = &t
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (d *Database) ListRevokedSharedFilePaths(accountID string) ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT f.local_path FROM shared_files f WHERE f.account_id=? AND f.revoked=1 AND f.local_path<>''
		AND NOT EXISTS (SELECT 1 FROM shared_files live WHERE live.account_id=f.account_id AND live.local_path=f.local_path AND live.revoked=0)`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (d *Database) ClearRevokedSharedFilePaths(accountID string) error {
	_, err := d.db.Exec(`UPDATE shared_files SET local_path='' WHERE account_id=? AND revoked=1 AND local_path<>''`, accountID)
	return err
}

// RecentlyRevokedSharedNotes lists notes invalidated by exactly the latest
// published generation. Older revoked rows must not emit on every sync.
func (d *Database) RecentlyRevokedSharedNotes(accountID string, currentGeneration int64) ([]string, error) {
	rows, err := d.db.Query(`SELECT server_note_id FROM shared_notes WHERE account_id=? AND revoked=1 AND generation=? AND content IS NOT NULL`, accountID, currentGeneration-1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (d *Database) DeleteSharedAccountRows(accountID string) error {
	for _, table := range []string{"shared_files", "shared_download_jobs", "shared_snapshot_items", "shared_notes", "shared_notebooks", "shared_sync_state", "shared_accounts"} {
		if _, err := d.db.Exec(`DELETE FROM `+table+` WHERE account_id=?`, accountID); err != nil {
			return err
		}
	}
	return nil
}

func sharedStagingItemID(item models.SharedSnapshotItem) string {
	if item.Notebook != nil {
		return item.Notebook.NotebookID
	}
	if item.Note != nil {
		return item.Note.NoteID
	}
	if item.File != nil {
		return item.File.NoteID + ":" + item.File.Kind + ":" + item.File.FileID
	}
	return ""
}

func (d *Database) CurrentSharedGeneration(accountID string) (int64, error) {
	var generation int64
	err := d.db.QueryRow(`SELECT generation FROM shared_sync_state WHERE account_id=?`, accountID).Scan(&generation)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return generation, err
}

type FailedSharedContentJob struct {
	ResourceID string
	Target     string
	Error      string
}

func (d *Database) FailedSharedContentJobs(accountID string) ([]FailedSharedContentJob, error) {
	rows, err := d.db.Query(`SELECT resource_id, target_version, error FROM shared_download_jobs WHERE account_id=? AND resource_type='content' AND error<>''`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FailedSharedContentJob
	for rows.Next() {
		var j FailedSharedContentJob
		if err := rows.Scan(&j.ResourceID, &j.Target, &j.Error); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (d *Database) ReadySharedAttachmentsByAge(accountID string) ([]*models.SharedFile, error) {
	rows, err := d.db.Query(`SELECT server_note_id,server_file_id,kind,title,size,target_version,COALESCE(local_path,''),COALESCE(cached_at,0) FROM shared_files
		WHERE account_id=? AND kind='attachment' AND revoked=0 AND cache_state='ready' ORDER BY COALESCE(cached_at,0) ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SharedFile
	for rows.Next() {
		var f models.SharedFile
		var cached int64
		if err := rows.Scan(&f.NoteID, &f.FileID, &f.Kind, &f.Title, &f.Size, &f.TargetVersion, &f.LocalPath, &cached); err != nil {
			return nil, err
		}
		f.AccountID = accountID
		t := time.Unix(cached, 0)
		f.CachedAt = &t
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (d *Database) MarkSharedFilePending(accountID string, f *models.SharedFile) error {
	_, err := d.db.Exec(`UPDATE shared_files SET local_path='',cache_state='pending',cached_at=NULL WHERE account_id=? AND server_note_id=? AND server_file_id=? AND kind=?`,
		accountID, f.NoteID, f.FileID, f.Kind)
	return err
}
