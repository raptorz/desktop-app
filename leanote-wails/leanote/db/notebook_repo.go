package db

import (
	"database/sql"
	"time"

	"leanote/models"
	"leanote/utils"
)

func (d *Database) InsertNotebook(nb *models.Notebook) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO notebooks (
			_id, notebook_id, server_notebook_id, user_id, parent_notebook_id,
			title, seq, number_notes, url_title, is_blog, is_trash, usn,
			is_dirty, local_is_new, local_is_delete, created_time, updated_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nb.ID, nb.NotebookID, nb.ServerNotebookID, nb.UserID, nb.ParentNotebookID,
		nb.Title, nb.Seq, nb.NumberNotes, nb.UrlTitle, nb.IsBlog, nb.IsTrash, nb.Usn,
		nb.IsDirty, nb.LocalIsNew, nb.LocalIsDelete, utils.TimeToUnix(nb.CreatedTime), utils.TimeToUnix(nb.UpdatedTime),
	)
	return err
}

func (d *Database) InsertNotebookTx(tx *sql.Tx, nb *models.Notebook) error {
	_, err := tx.Exec(`
		INSERT OR REPLACE INTO notebooks (
			_id, notebook_id, server_notebook_id, user_id, parent_notebook_id,
			title, seq, number_notes, url_title, is_blog, is_trash, usn,
			is_dirty, local_is_new, local_is_delete, created_time, updated_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nb.ID, nb.NotebookID, nb.ServerNotebookID, nb.UserID, nb.ParentNotebookID,
		nb.Title, nb.Seq, nb.NumberNotes, nb.UrlTitle, nb.IsBlog, nb.IsTrash, nb.Usn,
		nb.IsDirty, nb.LocalIsNew, nb.LocalIsDelete, utils.TimeToUnix(nb.CreatedTime), utils.TimeToUnix(nb.UpdatedTime),
	)
	return err
}

func (d *Database) GetNotebooks(userID string) ([]*models.Notebook, error) {
	rows, err := d.db.Query(`
		SELECT _id, notebook_id, server_notebook_id, user_id, parent_notebook_id,
			title, seq, number_notes, url_title, is_blog, is_trash, usn,
			is_dirty, local_is_new, local_is_delete, created_time, updated_time
		FROM notebooks 
		WHERE user_id = ? AND (local_is_delete = 0 OR local_is_delete IS NULL)
		ORDER BY seq
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotebooks(rows)
}

func (d *Database) GetNotebook(notebookID string) (*models.Notebook, error) {
	row := d.db.QueryRow(`
		SELECT _id, notebook_id, server_notebook_id, user_id, parent_notebook_id,
			title, seq, number_notes, url_title, is_blog, is_trash, usn,
			is_dirty, local_is_new, local_is_delete, created_time, updated_time
		FROM notebooks WHERE notebook_id = ?
	`, notebookID)

	return d.scanNotebook(row)
}

func (d *Database) GetNotebookByServerID(serverNotebookID string) (*models.Notebook, error) {
	row := d.db.QueryRow(`
		SELECT _id, notebook_id, server_notebook_id, user_id, parent_notebook_id,
			title, seq, number_notes, url_title, is_blog, is_trash, usn,
			is_dirty, local_is_new, local_is_delete, created_time, updated_time
		FROM notebooks WHERE server_notebook_id = ?
	`, serverNotebookID)

	return d.scanNotebook(row)
}

func (d *Database) GetNotebookIDByServerID(serverNotebookID string) (string, error) {
	row := d.db.QueryRow(`SELECT notebook_id FROM notebooks WHERE server_notebook_id = ?`, serverNotebookID)
	var notebookID string
	err := row.Scan(&notebookID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return notebookID, err
}

func (d *Database) GetServerNotebookID(notebookID string) (string, error) {
	row := d.db.QueryRow(`SELECT server_notebook_id FROM notebooks WHERE notebook_id = ?`, notebookID)
	var serverID string
	err := row.Scan(&serverID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return serverID, err
}

func (d *Database) UpdateNotebook(nb *models.Notebook) error {
	_, err := d.db.Exec(`
		UPDATE notebooks SET
			title = ?, parent_notebook_id = ?, seq = ?, is_dirty = ?, updated_time = ?
		WHERE notebook_id = ?
	`, nb.Title, nb.ParentNotebookID, nb.Seq, nb.IsDirty, time.Now().Unix(), nb.NotebookID)
	return err
}

func (d *Database) UpdateNotebookForce(nb *models.Notebook) error {
	_, err := d.db.Exec(`
		UPDATE notebooks SET
			title = ?, parent_notebook_id = ?, seq = ?, usn = ?,
			is_dirty = 0, local_is_new = 0, local_is_delete = 0
		WHERE notebook_id = ?
	`, nb.Title, nb.ParentNotebookID, nb.Seq, nb.Usn, nb.NotebookID)
	return err
}

func (d *Database) DeleteNotebook(notebookID string) error {
	_, err := d.db.Exec(`
		UPDATE notebooks SET is_dirty = 1, local_is_delete = 1, updated_time = ?
		WHERE notebook_id = ?
	`, time.Now().Unix(), notebookID)
	return err
}

func (d *Database) DeleteNotebookForce(notebookID string) error {
	_, err := d.db.Exec(`DELETE FROM notebooks WHERE notebook_id = ?`, notebookID)
	return err
}

func (d *Database) DeleteLocalNotebook(notebookID string) error {
	_, err := d.db.Exec(`DELETE FROM notebooks WHERE notebook_id = ?`, notebookID)
	return err
}

func (d *Database) GetDirtyNotebooks(userID string) ([]*models.Notebook, error) {
	rows, err := d.db.Query(`
		SELECT _id, notebook_id, server_notebook_id, user_id, parent_notebook_id,
			title, seq, number_notes, url_title, is_blog, is_trash, usn,
			is_dirty, local_is_new, local_is_delete, created_time, updated_time
		FROM notebooks WHERE user_id = ? AND is_dirty = 1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return d.scanNotebooks(rows)
}

func (d *Database) UpdateNotebookNumberNotes(notebookID string, count int) error {
	_, err := d.db.Exec(`UPDATE notebooks SET number_notes = ? WHERE notebook_id = ?`, count, notebookID)
	return err
}

func (d *Database) SetNotebookNotDirty(notebookID string) error {
	_, err := d.db.Exec(`UPDATE notebooks SET is_dirty = 0 WHERE notebook_id = ?`, notebookID)
	return err
}

func (d *Database) AddNotebookForce(nb *models.Notebook) (*models.Notebook, error) {
	if nb.ID == "" {
		nb.ID = utils.ObjectId()
	}
	if nb.NotebookID == "" {
		nb.NotebookID = nb.ID
	}
	if nb.ServerNotebookID == "" && nb.NotebookID != "" {
		nb.ServerNotebookID = nb.NotebookID
	}
	nb.IsDirty = false
	nb.LocalIsNew = false
	nb.LocalIsDelete = false

	err := d.InsertNotebook(nb)
	if err != nil {
		return nil, err
	}
	return nb, nil
}

func (d *Database) scanNotebook(row *sql.Row) (*models.Notebook, error) {
	var nb models.Notebook
	var createdTime, updatedTime sql.NullInt64

	err := row.Scan(
		&nb.ID, &nb.NotebookID, &nb.ServerNotebookID, &nb.UserID, &nb.ParentNotebookID,
		&nb.Title, &nb.Seq, &nb.NumberNotes, &nb.UrlTitle, &nb.IsBlog, &nb.IsTrash, &nb.Usn,
		&nb.IsDirty, &nb.LocalIsNew, &nb.LocalIsDelete, &createdTime, &updatedTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdTime.Valid {
		t := utils.UnixToTime(createdTime.Int64)
		nb.CreatedTime = &t
	}
	if updatedTime.Valid {
		t := utils.UnixToTime(updatedTime.Int64)
		nb.UpdatedTime = &t
	}

	return &nb, nil
}

func (d *Database) scanNotebooks(rows *sql.Rows) ([]*models.Notebook, error) {
	var notebooks []*models.Notebook
	for rows.Next() {
		var nb models.Notebook
		var createdTime, updatedTime sql.NullInt64

		err := rows.Scan(
			&nb.ID, &nb.NotebookID, &nb.ServerNotebookID, &nb.UserID, &nb.ParentNotebookID,
			&nb.Title, &nb.Seq, &nb.NumberNotes, &nb.UrlTitle, &nb.IsBlog, &nb.IsTrash, &nb.Usn,
			&nb.IsDirty, &nb.LocalIsNew, &nb.LocalIsDelete, &createdTime, &updatedTime,
		)
		if err != nil {
			return nil, err
		}

		if createdTime.Valid {
			t := utils.UnixToTime(createdTime.Int64)
			nb.CreatedTime = &t
		}
		if updatedTime.Valid {
			t := utils.UnixToTime(updatedTime.Int64)
			nb.UpdatedTime = &t
		}

		notebooks = append(notebooks, &nb)
	}
	return notebooks, nil
}

func (d *Database) MapNotebooks(notebooks []*models.Notebook) []*models.Notebook {
	notebooksMap := make(map[string]*models.Notebook)
	for _, nb := range notebooks {
		nb.Subs = []*models.Notebook{}
		notebooksMap[nb.NotebookID] = nb
	}

	var roots []*models.Notebook
	for _, nb := range notebooks {
		if nb.ParentNotebookID != "" && notebooksMap[nb.ParentNotebookID] != nil {
			notebooksMap[nb.ParentNotebookID].Subs = append(notebooksMap[nb.ParentNotebookID].Subs, nb)
		} else {
			roots = append(roots, nb)
		}
	}

	sortNotebooks(roots)
	return roots
}

func sortNotebooks(notebooks []*models.Notebook) {
	for i := 0; i < len(notebooks)-1; i++ {
		for j := i + 1; j < len(notebooks); j++ {
			if notebooks[i].Seq > notebooks[j].Seq {
				notebooks[i], notebooks[j] = notebooks[j], notebooks[i]
			}
		}
	}
	for _, nb := range notebooks {
		if len(nb.Subs) > 0 {
			sortNotebooks(nb.Subs)
		}
	}
}

func (d *Database) UpdateNotebookAfterSync(notebookID string, serverNb *models.Notebook) error {
	localParentID, _ := d.GetNotebookIDByServerID(serverNb.ParentNotebookID)

	_, err := d.db.Exec(`
		UPDATE notebooks SET
			server_notebook_id = ?, title = ?, parent_notebook_id = ?, seq = ?, usn = ?,
			is_dirty = 0, local_is_new = 0
		WHERE notebook_id = ?
	`, serverNb.NotebookID, serverNb.Title, localParentID, serverNb.Seq, serverNb.Usn, notebookID)
	return err
}
