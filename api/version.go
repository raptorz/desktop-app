package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ServerVersion struct {
	Server     string `json:"server"`
	Version    string `json:"version"`
	MinVersion string `json:"min_version"`
}

const ClientVersion = "1.0.0"

func ServerVersionNotice(info *ServerVersion, err error) string {
	if err != nil {
		var apiErr *SharedAPIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return "serverMigrationRequired"
		}
		return ""
	}
	if info == nil || info.Server != "pearlnote" {
		return "serverMigrationRequired"
	}
	if info.MinVersion != "" && CompareVersions(ClientVersion, info.MinVersion) < 0 {
		return "clientUpgradeRequired"
	}
	if info.Version != "" && CompareVersions(ClientVersion, info.Version) > 0 {
		return "serverUpgradeRequired"
	}
	return ""
}

// GetServerVersion is public and can be called before a token is available.
// A 404 is the expected response from an old Leanote server.
func (c *Client) GetServerVersion() (*ServerVersion, error) {
	resp, err := c.get("system/version", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, &SharedAPIError{Status: resp.StatusCode(), Msg: "versionNotFound"}
	}
	var info ServerVersion
	if err := json.Unmarshal(resp.Body(), &info); err != nil {
		return nil, fmt.Errorf("invalid server version response: %w", err)
	}
	return &info, nil
}

// CompareVersions compares numeric dotted versions and returns -1, 0, or 1.
func CompareVersions(a, b string) int {
	parse := func(v string) []int {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
		out := make([]int, len(parts))
		for i, part := range parts {
			n := 0
			for _, r := range part {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
			}
			out[i] = n
		}
		return out
	}
	x, y := parse(a), parse(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var xv, yv int
		if i < len(x) {
			xv = x[i]
		}
		if i < len(y) {
			yv = y[i]
		}
		if xv < yv {
			return -1
		}
		if xv > yv {
			return 1
		}
	}
	return 0
}
