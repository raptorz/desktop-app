package utils

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

func ObjectId() string {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b[0:4], uint32(time.Now().Unix()))
	rand.Read(b[4:12])
	return hex.EncodeToString(b)
}

func UUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func MD5(s string) string {
	h := NewMD5Hash()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func MD5WithSalt(s, salt string) string {
	return MD5(s + salt)
}

func IsValidObjectId(s string) bool {
	if len(s) != 24 {
		return false
	}
	matched, _ := regexp.MatchString("^[0-9a-fA-F]{24}$", s)
	return matched
}

func InArray(arr []string, item string) bool {
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}

func IsImageExt(ext string) bool {
	ext = stringsToLower(ext)
	imageExts := map[string]bool{
		"gif":  true,
		"jpeg": true,
		"jpg":  true,
		"png":  true,
		"svg":  true,
		"webp": true,
		"bmp":  true,
		"ico":  true,
	}
	return imageExts[ext]
}

func stringsToLower(s string) string {
	b := make([]byte, len(s))
	for i := range b {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func GetMIMEType(ext string) string {
	ext = stringsToLower(ext)
	mines := map[string]string{
		"gif":  "image/gif",
		"jpeg": "image/jpeg",
		"jpg":  "image/jpeg",
		"png":  "image/png",
		"svg":  "image/svg+xml",
		"webp": "image/webp",
		"bmp":  "image/bmp",
		"ico":  "image/x-icon",
		"css":  "text/css",
		"html": "text/html",
		"htm":  "text/html",
		"js":   "application/javascript",
		"json": "application/json",
		"pdf":  "application/pdf",
		"txt":  "text/plain",
		"xml":  "application/xml",
		"zip":  "application/zip",
		"tar":  "application/x-tar",
		"gz":   "application/gzip",
		"mp3":  "audio/mpeg",
		"mp4":  "video/mp4",
		"wav":  "audio/wav",
		"wma":  "audio/x-ms-wma",
		"wmv":  "video/x-ms-wmv",
		"avi":  "video/x-msvideo",
		"doc":  "application/msword",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"xls":  "application/vnd.ms-excel",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"ppt":  "application/vnd.ms-powerpoint",
		"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	if t, ok := mines[ext]; ok {
		return t
	}
	return "application/octet-stream"
}
