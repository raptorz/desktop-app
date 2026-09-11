package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"

	"github.com/gemsnote/gemsnote/models"
)

type SharedAPIError struct {
	Status int
	Msg    string
}

func (e *SharedAPIError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("shared API returned HTTP %d", e.Status)
}

func IsSharedPermissionDenied(err error) bool {
	var apiErr *SharedAPIError
	return errors.As(err, &apiErr) && apiErr.Msg == "noPermission"
}

func IsSharedUnsupported(err error) bool {
	var apiErr *SharedAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == 404 || apiErr.Msg == "unsupported" || apiErr.Msg == "notFound"
}

type SharedCapabilities struct {
	Ok              bool   `json:"Ok"`
	Msg             string `json:"Msg"`
	ProtocolVersion int    `json:"ProtocolVersion"`
	Snapshot        bool   `json:"Snapshot"`
}

type SharedSnapshot struct {
	Ok         bool   `json:"Ok"`
	Msg        string `json:"Msg"`
	SnapshotID string `json:"SnapshotId"`
	ExpiresAt  string `json:"ExpiresAt"`
	Total      int    `json:"Total"`
}

type SharedSnapshotPage struct {
	Ok            bool                        `json:"Ok"`
	Msg           string                      `json:"Msg"`
	Items         []models.SharedSnapshotItem `json:"Items"`
	NextPageToken string                      `json:"NextPageToken"`
	Complete      bool                        `json:"Complete"`
	Total         int                         `json:"Total"`
}

type SharedContent struct {
	Ok      bool   `json:"Ok"`
	Msg     string `json:"Msg"`
	NoteID  string `json:"NoteId"`
	Version string `json:"Version"`
	Digest  string `json:"Digest"`
	Content string `json:"Content"`
}

func decodeShared(respBody []byte, status int, out interface{}) error {
	if status < 200 || status >= 300 {
		var envelope struct {
			Msg string `json:"Msg"`
		}
		_ = json.Unmarshal(respBody, &envelope)
		return &SharedAPIError{Status: status, Msg: envelope.Msg}
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("invalid shared API response: %w", err)
	}
	return nil
}

func (c *Client) GetSharedCapabilities() (*SharedCapabilities, error) {
	resp, err := c.get("shared/capabilities", nil)
	if err != nil {
		return nil, err
	}
	var result SharedCapabilities
	if err := decodeShared(resp.Body(), resp.StatusCode(), &result); err != nil {
		return nil, err
	}
	if !result.Ok || !result.Snapshot {
		return nil, &SharedAPIError{Status: resp.StatusCode(), Msg: result.Msg}
	}
	return &result, nil
}

func (c *Client) CreateSharedSnapshot() (*SharedSnapshot, error) {
	resp, err := c.post("shared/snapshots", map[string]string{}, nil)
	if err != nil {
		return nil, err
	}
	var result SharedSnapshot
	if err := decodeShared(resp.Body(), resp.StatusCode(), &result); err != nil {
		return nil, err
	}
	if !result.Ok || result.SnapshotID == "" {
		return nil, fmt.Errorf("create shared snapshot failed: %s", result.Msg)
	}
	return &result, nil
}

func (c *Client) GetSharedSnapshotPage(snapshotID, pageToken string) (*SharedSnapshotPage, error) {
	resp, err := c.get("shared/snapshots/"+url.PathEscape(snapshotID)+"/items", map[string]string{"pageToken": pageToken})
	if err != nil {
		return nil, err
	}
	var result SharedSnapshotPage
	if err := decodeShared(resp.Body(), resp.StatusCode(), &result); err != nil {
		return nil, err
	}
	if !result.Ok {
		return nil, fmt.Errorf("read shared snapshot failed: %s", result.Msg)
	}
	return &result, nil
}

func (c *Client) GetSharedNoteContent(noteID string) (*SharedContent, error) {
	resp, err := c.get("shared/notes/"+url.PathEscape(noteID)+"/content", nil)
	if err != nil {
		return nil, err
	}
	var result SharedContent
	if err := decodeShared(resp.Body(), resp.StatusCode(), &result); err != nil {
		return nil, err
	}
	if !result.Ok {
		return nil, fmt.Errorf("read shared content failed: %s", result.Msg)
	}
	return &result, nil
}

func (c *Client) GetSharedNoteFile(noteID, fileID string) ([]byte, string, error) {
	resp, err := c.get("shared/notes/"+url.PathEscape(noteID)+"/files/"+url.PathEscape(fileID), nil)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, "", decodeShared(resp.Body(), resp.StatusCode(), &struct{}{})
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header().Get("Content-Type"))
	if mediaType == "application/json" {
		var envelope struct {
			Ok  bool   `json:"Ok"`
			Msg string `json:"Msg"`
		}
		if err := json.Unmarshal(resp.Body(), &envelope); err != nil {
			return nil, "", fmt.Errorf("invalid shared file response: %w", err)
		}
		if !envelope.Ok {
			return nil, "", &SharedAPIError{Status: resp.StatusCode(), Msg: envelope.Msg}
		}
		return nil, "", fmt.Errorf("unexpected JSON shared file response")
	}
	return resp.Body(), resp.Header().Get("X-Gemsnote-SHA256"), nil
}
