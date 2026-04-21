package db

import (
	"database/sql"

	"leanote/models"
	"leanote/utils"
)

func (d *Database) InsertTag(tag *models.Tag) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO tags (_id, tag, user_id, count, usn, is_dirty, local_is_delete, created_time, updated_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tag.ID, tag.Tag, tag.UserID, tag.Count, tag.Usn, tag.IsDirty, tag.LocalIsDelete,
		utils.TimeToUnix(tag.CreatedTime), utils.TimeToUnix(tag.UpdatedTime))
	return err
}

func (d *Database) InsertTagTx(tx *sql.Tx, tag *models.Tag) error {
	_, err := tx.Exec(`
		INSERT OR REPLACE INTO tags (_id, tag, user_id, count, usn, is_dirty, local_is_delete, created_time, updated_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tag.ID, tag.Tag, tag.UserID, tag.Count, tag.Usn, tag.IsDirty, tag.LocalIsDelete,
		utils.TimeToUnix(tag.CreatedTime), utils.TimeToUnix(tag.UpdatedTime))
	return err
}

func (d *Database) GetTags(userID string) ([]*models.Tag, error) {
	rows, err := d.db.Query(`
		SELECT _id, tag, user_id, count, usn, is_dirty, local_is_delete, created_time, updated_time
		FROM tags WHERE user_id = ? AND (local_is_delete = 0 OR local_is_delete IS NULL)
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanTags(rows)
}

func (d *Database) GetTag(userID, tagName string) (*models.Tag, error) {
	row := d.db.QueryRow(`
		SELECT _id, tag, user_id, count, usn, is_dirty, local_is_delete, created_time, updated_time
		FROM tags WHERE user_id = ? AND tag = ?
	`, userID, tagName)

	var tag models.Tag
	var createdTime, updatedTime sql.NullInt64

	err := row.Scan(&tag.ID, &tag.Tag, &tag.UserID, &tag.Count, &tag.Usn, &tag.IsDirty, &tag.LocalIsDelete, &createdTime, &updatedTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		tag.CreatedTime = &t
	}
	if updatedTime.Valid {
		t := utils.UnixToTime(updatedTime.Int64)
		tag.UpdatedTime = &t
	}

	return &tag, nil
}

func (d *Database) AddOrUpdateTag(userID, tagName string, isSync bool, usn int64) (*models.Tag, error) {
	existing, err := d.GetTag(userID, tagName)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if isSync {
			_, err = d.db.Exec(`UPDATE tags SET is_dirty = 0, usn = ? WHERE tag = ? AND user_id = ?`, usn, tagName, userID)
		}
		return existing, err
	}

	tag := &models.Tag{
		ID:      utils.ObjectId(),
		Tag:     tagName,
		UserID:  userID,
		IsDirty: true,
	}
	if isSync {
		tag.IsDirty = false
		tag.Usn = usn
	}

	err = d.InsertTag(tag)
	return tag, err
}

func (d *Database) DeleteTag(userID, tagName string) error {
	_, err := d.db.Exec(`UPDATE tags SET is_dirty = 1, local_is_delete = 1 WHERE tag = ? AND user_id = ?`, tagName, userID)
	return err
}

func (d *Database) DeleteTagForce(tagName string) error {
	_, err := d.db.Exec(`DELETE FROM tags WHERE tag = ?`, tagName)
	return err
}

func (d *Database) DeleteLocalTag(tagName string) error {
	_, err := d.db.Exec(`DELETE FROM tags WHERE tag = ?`, tagName)
	return err
}

func (d *Database) SetTagNotDirty(tagName string) error {
	_, err := d.db.Exec(`UPDATE tags SET is_dirty = 0 WHERE tag = ?`, tagName)
	return err
}

func (d *Database) SetTagNotDirtyAndUsn(tagName string, usn int64) error {
	_, err := d.db.Exec(`UPDATE tags SET is_dirty = 0, usn = ? WHERE tag = ?`, usn, tagName)
	return err
}

func (d *Database) UpdateTagCount(tagName string, count int) error {
	_, err := d.db.Exec(`UPDATE tags SET count = ? WHERE tag = ?`, count, tagName)
	return err
}

func (d *Database) GetDirtyTags(userID string) ([]*models.Tag, error) {
	rows, err := d.db.Query(`
		SELECT _id, tag, user_id, count, usn, is_dirty, local_is_delete, created_time, updated_time
		FROM tags WHERE user_id = ? AND is_dirty = 1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanTags(rows)
}

func (d *Database) scanTags(rows *sql.Rows) ([]*models.Tag, error) {
	var tags []*models.Tag
	for rows.Next() {
		var tag models.Tag
		var createdTime, updatedTime sql.NullInt64

		err := rows.Scan(&tag.ID, &tag.Tag, &tag.UserID, &tag.Count, &tag.Usn, &tag.IsDirty, &tag.LocalIsDelete, &createdTime, &updatedTime)
		if err != nil {
			return nil, err
		}

		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			tag.CreatedTime = &t
		}
		if updatedTime.Valid {
			t := utils.UnixToTime(updatedTime.Int64)
			tag.UpdatedTime = &t
		}

		tags = append(tags, &tag)
	}
	return tags, nil
}
