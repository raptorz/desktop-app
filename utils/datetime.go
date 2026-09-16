package utils

import (
	"strings"
	"time"
)

func GoNowToDate(goNow string) time.Time {
	if goNow == "" {
		return time.Now()
	}

	layouts := []string{
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, goNow); err == nil {
			return t
		}
	}

	return time.Now()
}

func FormatDatetime(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.Format("2006-01-02 15:04:05")
}

func TimeToUnix(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}

func UnixToTime(unix int64) time.Time {
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

func FixNoteContent(content, serverURL, localURL string) string {
	if content == "" {
		return content
	}

	replacements := map[string]string{
		strings.Replace(serverURL, "https", "https*", 1) + "/api2/file/outputImage": localURL,
		serverURL + "/api2/file/getImage":                                       localURL,
		serverURL + "/api2/file/getAttach":                                      localURL,
		serverURL + "/api2/attach/download?attachId":                                localURL + "?fileId",
	}

	for old, new := range replacements {
		content = strings.ReplaceAll(content, old, new)
	}
	return content
}

func FixNoteContentForSend(content, serverURL, localURL string) string {
	if content == "" {
		return content
	}

	replacements := map[string]string{
		localURL:                  serverURL + "/api2/file/getImage",
		"leanote://file/getImage": serverURL + "/api2/file/getImage",
	}

	for old, new := range replacements {
		content = strings.ReplaceAll(content, old, new)
	}
	return content
}
