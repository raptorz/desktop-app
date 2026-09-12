package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

type AuthResponse struct {
	Ok       bool   `json:"Ok"`
	Msg      string `json:"Msg"`
	UserID   string `json:"UserId"`
	Username string `json:"Username"`
	Email    string `json:"Email"`
	Token    string `json:"Token"`
}

func (c *Client) Auth(email, password, host string) (*AuthResponse, error) {
	c.SetHost(host)

	resp, err := c.post("auth/login", map[string]string{
		"email": email,
		"pwd":   password,
	}, nil)
	if err != nil {
		return nil, err
	}

	var authResp AuthResponse
	if err := json.Unmarshal(resp.Body(), &authResp); err != nil {
		return nil, err
	}

	if authResp.Ok {
		c.SetToken(authResp.Token)
	}

	return &authResp, nil
}

func (c *Client) Logout() error {
	_, err := c.post("auth/logout", nil, nil)
	return err
}

type LastSyncStateResponse struct {
	Ok           bool   `json:"Ok"`
	LastSyncUsn  int64  `json:"LastSyncUsn"`
	LastSyncTime string `json:"LastSyncTime"`
	Msg          string `json:"Msg"`
}

func (c *Client) GetLastSyncState() (*LastSyncStateResponse, error) {
	resp, err := c.get("user/getSyncState", nil)
	if err != nil {
		return nil, err
	}

	var state LastSyncStateResponse
	if err := json.Unmarshal(resp.Body(), &state); err != nil {
		return nil, err
	}

	return &state, nil
}

func (c *Client) GetSyncNotebooks(afterUsn int64, maxEntry int) ([]*models.Notebook, error) {
	resp, err := c.get("notebook/getSyncNotebooks", map[string]string{
		"afterUsn": strconv.FormatInt(afterUsn, 10),
		"maxEntry": strconv.Itoa(maxEntry),
	})
	if err != nil {
		return nil, err
	}

	var notebooks []*models.Notebook
	if err := json.Unmarshal(resp.Body(), &notebooks); err != nil {
		return nil, err
	}

	return notebooks, nil
}

func (c *Client) GetSyncNotes(afterUsn int64, maxEntry int) ([]*models.Note, error) {
	resp, err := c.get("note/getSyncNotes", map[string]string{
		"afterUsn": strconv.FormatInt(afterUsn, 10),
		"maxEntry": strconv.Itoa(maxEntry),
	})
	if err != nil {
		return nil, err
	}

	var notes []*models.Note
	if err := json.Unmarshal(resp.Body(), &notes); err != nil {
		return nil, err
	}

	return notes, nil
}

func (c *Client) GetSyncTags(afterUsn int64, maxEntry int) ([]*models.Tag, error) {
	resp, err := c.get("tag/getSyncTags", map[string]string{
		"afterUsn": strconv.FormatInt(afterUsn, 10),
		"maxEntry": strconv.Itoa(maxEntry),
	})
	if err != nil {
		return nil, err
	}

	var tags []*models.Tag
	if err := json.Unmarshal(resp.Body(), &tags); err != nil {
		return nil, err
	}

	return tags, nil
}

func (c *Client) GetNoteContent(noteID string) (string, error) {
	resp, err := c.get("note/getNoteContent", map[string]string{
		"noteId": noteID,
	})
	if err != nil {
		return "", err
	}

	var contentResp models.NoteContentAPI
	if err := json.Unmarshal(resp.Body(), &contentResp); err != nil {
		return "", err
	}

	return contentResp.Content, nil
}

func (c *Client) GetNote(noteID string) (*models.Note, error) {
	resp, err := c.get("note/getNote", map[string]string{
		"noteId": noteID,
	})
	if err != nil {
		return nil, err
	}

	var note models.Note
	if err := json.Unmarshal(resp.Body(), &note); err != nil {
		return nil, err
	}

	return &note, nil
}

func (c *Client) GetImage(fileID, localPath string) (string, error) {
	resp, err := c.get("file/getImage", map[string]string{
		"fileId": fileID,
	})
	if err != nil {
		return "", err
	}

	contentType := resp.Header().Get("Content-Type")
	ext := "png"
	if contentType != "" {
		parts := strings.Split(contentType, "/")
		if len(parts) > 1 {
			ext = parts[1]
		}
	}

	filename := utils.UUID() + "." + ext
	fullPath := filepath.Join(localPath, filename)

	if err := os.WriteFile(fullPath, resp.Body(), 0644); err != nil {
		return "", err
	}

	return fullPath, nil
}

func (c *Client) GetAttach(fileID, localPath string) (string, string, error) {
	resp, err := c.get("file/getAttach", map[string]string{
		"fileId": fileID,
	})
	if err != nil {
		return "", "", err
	}

	contentDisposition := resp.Header().Get("Content-Disposition")

	var ext, filename string
	if contentDisposition != "" {
		re := regexp.MustCompile(`filename="(.+?)"`)
		matches := re.FindStringSubmatch(contentDisposition)
		if len(matches) > 1 {
			filename = matches[1]
			parts := strings.Split(filename, ".")
			if len(parts) > 1 {
				ext = parts[len(parts)-1]
			}
		}
	}

	if filename == "" {
		filename = utils.UUID()
		if ext != "" {
			filename += "." + ext
		}
	}

	fullPath := filepath.Join(localPath, filename)

	if err := os.WriteFile(fullPath, resp.Body(), 0644); err != nil {
		return "", "", err
	}

	return fullPath, filename, nil
}

type APIResponse struct {
	Ok       bool             `json:"Ok"`
	Msg      string           `json:"Msg"`
	Notebook *models.Notebook `json:",omitempty"`
	Note     *models.Note     `json:",omitempty"`
	Usn      int64            `json:"Usn"`
}

type HistoryListResponse struct {
	Ok   bool                 `json:"Ok"`
	Msg  string               `json:"Msg"`
	Item []models.HistoryMeta `json:"Item"`
}

type HistoryContentResponse struct {
	Ok   bool                 `json:"Ok"`
	Msg  string               `json:"Msg"`
	Item *models.HistoryEntry `json:"Item"`
}

// GetHistories returns history metadata. The server intentionally does not
// include the (potentially large) content in this response.
func (c *Client) GetHistories(noteID string) ([]models.HistoryMeta, error) {
	resp, err := c.get("note/getHistories", map[string]string{"noteId": noteID})
	if err != nil {
		return nil, err
	}
	var result HistoryListResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}
	if !result.Ok {
		return nil, fmt.Errorf("get histories failed: %s", result.Msg)
	}
	return result.Item, nil
}

// GetHistoryContent fetches one history entry. New servers identify versions
// by the stable historyId; index is retained for old Leanote compatibility.
func (c *Client) GetHistoryContent(noteID string, index int) (*models.HistoryEntry, error) {
	return c.GetHistoryContentByID(noteID, "", index)
}

// GetHistoryContentByID fetches a history entry by stable ID when available.
// The index fallback is needed for old servers and old local history records.
func (c *Client) GetHistoryContentByID(noteID, historyID string, index int) (*models.HistoryEntry, error) {
	params := map[string]string{"noteId": noteID}
	if historyID != "" {
		params["historyId"] = historyID
	} else {
		params["index"] = strconv.Itoa(index)
	}
	resp, err := c.get("note/getHistoryContent", params)
	if err != nil {
		return nil, err
	}
	var result HistoryContentResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, err
	}
	if !result.Ok {
		return nil, fmt.Errorf("get history content failed: %s", result.Msg)
	}
	return result.Item, nil
}

func (c *Client) AddNotebook(nb *models.Notebook) (*models.Notebook, error) {
	resp, err := c.post("notebook/addNotebook", map[string]string{
		"title":            nb.Title,
		"seq":              strconv.Itoa(int(nb.Seq)),
		"parentNotebookId": nb.ParentNotebookID,
	}, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return apiResp.Notebook, nil
}

func (c *Client) UpdateNotebook(nb *models.Notebook) (*models.Notebook, error) {
	resp, err := c.post("notebook/updateNotebook", map[string]string{
		"notebookId":       nb.ServerNotebookID,
		"title":            nb.Title,
		"usn":              strconv.FormatInt(nb.Usn, 10),
		"seq":              strconv.Itoa(int(nb.Seq)),
		"parentNotebookId": nb.ParentNotebookID,
	}, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return apiResp.Notebook, nil
}

func (c *Client) DeleteNotebook(nb *models.Notebook) (*APIResponse, error) {
	resp, err := c.post("notebook/deleteNotebook", map[string]string{
		"notebookId": nb.ServerNotebookID,
		"usn":        strconv.FormatInt(nb.Usn, 10),
	}, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return &apiResp, nil
}

func (c *Client) AddNote(note *models.Note) (*models.Note, error) {
	data := map[string]interface{}{
		"Title":      note.Title,
		"NotebookId": note.NotebookID,
		"Content":    note.Content,
		"IsMarkdown": note.IsMarkdown,
		"Tags":       note.Tags,
		"IsBlog":     note.IsBlog,
		"IsStar":     note.IsStar,
		"Files":      note.Files,
		"FileDatas":  note.FileDatas,
	}

	resp, err := c.post("note/addNote", data, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return apiResp.Note, nil
}

func (c *Client) UpdateNote(note *models.Note) (*models.Note, error) {
	data := map[string]interface{}{
		"NoteId":     note.ServerNoteID,
		"NotebookId": note.NotebookID,
		"Title":      note.Title,
		"Usn":        note.Usn,
		"IsTrash":    note.IsTrash,
		"IsBlog":     note.IsBlog,
		"IsStar":     note.IsStar,
		"Tags":       note.Tags,
		"Files":      note.Files,
		"FileDatas":  note.FileDatas,
	}

	if note.ContentIsDirty {
		data["Content"] = note.Content
		if note.IsMarkdown {
			data["Abstract"] = note.Abstract
		}
	}

	resp, err := c.post("note/updateNote", data, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return apiResp.Note, nil
}

func (c *Client) DeleteTrash(note *models.Note) (*APIResponse, error) {
	resp, err := c.post("note/deleteTrash", map[string]string{
		"noteId": note.ServerNoteID,
		"usn":    strconv.FormatInt(note.Usn, 10),
	}, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return &apiResp, nil
}

func (c *Client) AddTag(title string) (*models.Tag, error) {
	resp, err := c.post("tag/addTag", map[string]string{
		"tag": title,
	}, nil)
	if err != nil {
		return nil, err
	}

	var tag models.Tag
	if err := json.Unmarshal(resp.Body(), &tag); err != nil {
		return nil, err
	}

	return &tag, nil
}

func (c *Client) DeleteTag(tag *models.Tag) (*APIResponse, error) {
	resp, err := c.post("tag/deleteTag", map[string]string{
		"tag": tag.Tag,
		"usn": strconv.FormatInt(tag.Usn, 10),
	}, nil)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(resp.Body(), &apiResp); err != nil {
		return nil, err
	}

	return &apiResp, nil
}
