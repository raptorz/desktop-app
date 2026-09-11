package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/utils"
)

type FileService struct {
	db      *db.Database
	dataDir string
}

func getDataPath() string {
	homeDir, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Application Support", "gemsnote")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		return filepath.Join(appData, "gemsnote")
	default:
		configDir := os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(homeDir, ".config")
		}
		return filepath.Join(configDir, "gemsnote")
	}
}

func NewFileService(database *db.Database) *FileService {
	dataDir := filepath.Join(getDataPath(), "data")
	os.MkdirAll(dataDir, 0755)

	return &FileService{
		db:      database,
		dataDir: dataDir,
	}
}

func (fs *FileService) GetDataDir() string {
	return fs.dataDir
}

func (fs *FileService) SetDataDir(dir string) {
	fs.dataDir = dir
	os.MkdirAll(dir, 0755)
}

func (fs *FileService) GetUserDir(userID string) string {
	return filepath.Join(fs.dataDir, "users", userID)
}

func (fs *FileService) GetImageDir() string {
	user, _ := fs.db.GetActiveUser()
	if user != nil {
		return fs.GetUserImageDir(user.ID)
	}
	return filepath.Join(fs.dataDir, "images")
}

func (fs *FileService) GetUserImageDir(userID string) string {
	dir := filepath.Join(fs.GetUserDir(userID), "images")
	os.MkdirAll(dir, 0755)
	return dir
}

func (fs *FileService) GetAttachDir() string {
	user, _ := fs.db.GetActiveUser()
	if user != nil {
		return fs.GetUserAttachDir(user.ID)
	}
	return filepath.Join(fs.dataDir, "attachs")
}

func (fs *FileService) GetUserAttachDir(userID string) string {
	dir := filepath.Join(fs.GetUserDir(userID), "attachs")
	os.MkdirAll(dir, 0755)
	return dir
}

func (fs *FileService) GetUserDBPath(userID string) string {
	return filepath.Join(fs.GetUserDir(userID), "data.db")
}

func (fs *FileService) GetImagePath(fileID string) string {
	return filepath.Join(fs.GetImageDir(), fileID)
}

func (fs *FileService) GetAttachPath(fileID string) string {
	return filepath.Join(fs.GetAttachDir(), fileID)
}

func (fs *FileService) InitUserDirs(userID string) error {
	userDir := fs.GetUserDir(userID)
	os.MkdirAll(filepath.Join(userDir, "images"), 0755)
	os.MkdirAll(filepath.Join(userDir, "attachs"), 0755)
	return nil
}

func (fs *FileService) DeleteUserDir(userID string) error {
	userDir := fs.GetUserDir(userID)
	return os.RemoveAll(userDir)
}

func (fs *FileService) GetUserImagesPath(userID string) string {
	return filepath.Join(fs.GetUserDir(userID), "images")
}

func (fs *FileService) GetUserAttachsPath(userID string) string {
	return filepath.Join(fs.GetUserDir(userID), "attachs")
}

func (fs *FileService) GetDataStats(userID string) map[string]int64 {
	userDir := fs.GetUserDir(userID)
	stats := make(map[string]int64)

	imagesDir := filepath.Join(userDir, "images")
	if info, err := os.Stat(imagesDir); err == nil && info.IsDir() {
		stats["images"] = getDirSize(imagesDir)
	}

	attachsDir := filepath.Join(userDir, "attachs")
	if info, err := os.Stat(attachsDir); err == nil && info.IsDir() {
		stats["attachs"] = getDirSize(attachsDir)
	}

	dbPath := fs.GetUserDBPath(userID)
	if info, err := os.Stat(dbPath); err == nil {
		stats["db"] = info.Size() / 1024
	}

	return stats
}

func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size / 1024
}

func (fs *FileService) PasteImage(base64Data string) (map[string]interface{}, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, fmt.Errorf("no active user")
	}

	data := base64Data
	if strings.Contains(base64Data, ",") {
		parts := strings.SplitN(base64Data, ",", 2)
		data = parts[1]
	}

	imgBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 data: %w", err)
	}

	fileID := utils.ObjectId()
	ext := "png"
	filename := fileID + "." + ext
	imageDir := fs.GetUserImageDir(user.ID)
	fullPath := filepath.Join(imageDir, filename)

	if err := os.WriteFile(fullPath, imgBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to write image: %w", err)
	}

	now := time.Now()
	img := &models.Image{
		ID:          utils.ObjectId(),
		FileID:      fileID,
		UserID:      user.ID,
		Path:        fullPath,
		IsDirty:     true,
		CreatedTime: &now,
	}

	if err := fs.db.InsertImage(img); err != nil {
		os.Remove(fullPath)
		return nil, fmt.Errorf("failed to save image record: %w", err)
	}

	return map[string]interface{}{
		"FileId": fileID,
		"Path":   fullPath,
		"Url":    "leanote://file/getImage?fileId=" + fileID,
	}, nil
}

func (fs *FileService) CopyFile(srcPath string, isImage bool) (map[string]interface{}, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, fmt.Errorf("no active user")
	}

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("source file not found: %s", srcPath)
	}

	fileID := utils.ObjectId()
	ext := filepath.Ext(srcPath)
	if len(ext) > 0 {
		ext = ext[1:]
	}

	var targetDir, fileType string
	if isImage {
		targetDir = fs.GetUserImageDir(user.ID)
		fileType = "image"
	} else {
		targetDir = fs.GetUserAttachDir(user.ID)
		fileType = "attach"
	}

	filename := fileID + "." + ext
	fullPath := filepath.Join(targetDir, filename)

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(fullPath)
		return nil, fmt.Errorf("failed to copy file: %w", err)
	}

	now := time.Now()
	if isImage {
		img := &models.Image{
			ID:          utils.ObjectId(),
			FileID:      fileID,
			UserID:      user.ID,
			Path:        fullPath,
			IsDirty:     true,
			CreatedTime: &now,
		}
		if err := fs.db.InsertImage(img); err != nil {
			os.Remove(fullPath)
			return nil, err
		}
		return map[string]interface{}{
			"FileId":  fileID,
			"Path":    fullPath,
			"Url":     "leanote://file/getImage?fileId=" + fileID,
			"IsImage": true,
		}, nil
	}

	attach := &models.Attach{
		ID:          utils.ObjectId(),
		FileID:      fileID,
		UserID:      user.ID,
		Title:       filepath.Base(srcPath),
		Type:        ext,
		Path:        fullPath,
		IsAttach:    true,
		IsDirty:     true,
		CreatedTime: &now,
	}
	if err := fs.db.InsertAttach(attach); err != nil {
		os.Remove(fullPath)
		return nil, err
	}

	return map[string]interface{}{
		"FileId":   fileID,
		"Path":     fullPath,
		"Title":    attach.Title,
		"Type":     fileType,
		"IsAttach": true,
	}, nil
}

func (fs *FileService) AddAttach(srcPath, noteID string) (map[string]interface{}, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, fmt.Errorf("no active user")
	}

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("source file not found: %s", srcPath)
	}

	fileID := utils.ObjectId()
	ext := filepath.Ext(srcPath)
	if len(ext) > 0 {
		ext = ext[1:]
	}

	title := filepath.Base(srcPath)
	filename := utils.UUID() + "." + ext
	attachDir := fs.GetUserAttachDir(user.ID)
	fullPath := filepath.Join(attachDir, filename)

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(fullPath)
	if err != nil {
		return nil, err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(fullPath)
		return nil, err
	}

	now := time.Now()
	attach := &models.Attach{
		ID:          utils.ObjectId(),
		FileID:      fileID,
		NoteID:      noteID,
		UserID:      user.ID,
		Title:       title,
		Type:        ext,
		Path:        fullPath,
		IsAttach:    true,
		IsDirty:     true,
		CreatedTime: &now,
	}

	if err := fs.db.InsertAttach(attach); err != nil {
		os.Remove(fullPath)
		return nil, err
	}

	return map[string]interface{}{
		"FileId": fileID,
		"Title":  title,
		"Type":   ext,
		"Path":   fullPath,
		"Url":    "leanote://file/getAttach?fileId=" + fileID,
	}, nil
}

func (fs *FileService) GetImage(fileID string) (string, error) {
	img, err := fs.db.GetImage(fileID)
	if err != nil {
		return "", err
	}
	if img != nil && img.Path != "" {
		if _, err := os.Stat(img.Path); err == nil {
			return img.Path, nil
		}
	}
	return "", fmt.Errorf("image not found: %s", fileID)
}

func (fs *FileService) AddImageForce(fileID, path string) error {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return fmt.Errorf("no active user")
	}

	fs.db.DeleteImage(fileID)

	now := time.Now()
	img := &models.Image{
		ID:           utils.ObjectId(),
		FileID:       fileID,
		ServerFileID: fileID,
		UserID:       user.ID,
		Path:         path,
		IsDirty:      false,
		CreatedTime:  &now,
	}
	return fs.db.InsertImage(img)
}

func (fs *FileService) GetAttach(fileID string) (string, string, error) {
	attach, err := fs.db.GetAttach(fileID)
	if err != nil {
		return "", "", err
	}
	if attach != nil && attach.Path != "" {
		if _, err := os.Stat(attach.Path); err == nil {
			return attach.Path, attach.Title, nil
		}
	}
	return "", "", fmt.Errorf("attachment not found: %s", fileID)
}

func (fs *FileService) GetAttachsByNote(noteID string) ([]*models.Attach, error) {
	return fs.db.GetAttachsByNote(noteID)
}

func (fs *FileService) DeleteAttach(fileID string) error {
	attach, err := fs.db.GetAttach(fileID)
	if err != nil {
		return err
	}
	if attach != nil && attach.Path != "" {
		os.Remove(attach.Path)
	}
	return fs.db.DeleteAttach(fileID)
}

func (fs *FileService) DownloadImage(url string) (map[string]interface{}, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, fmt.Errorf("no active user")
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to download image: status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	ext := "png"
	if contentType != "" {
		parts := strings.Split(contentType, "/")
		if len(parts) > 1 {
			ext = parts[1]
		}
	}

	fileID := utils.ObjectId()
	filename := fileID + "." + ext
	imageDir := fs.GetUserImageDir(user.ID)
	fullPath := filepath.Join(imageDir, filename)

	file, err := os.Create(fullPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		os.Remove(fullPath)
		return nil, err
	}

	now := time.Now()
	img := &models.Image{
		ID:          utils.ObjectId(),
		FileID:      fileID,
		UserID:      user.ID,
		Path:        fullPath,
		IsDirty:     true,
		CreatedTime: &now,
	}

	if err := fs.db.InsertImage(img); err != nil {
		os.Remove(fullPath)
		return nil, err
	}

	return map[string]interface{}{
		"FileId": fileID,
		"Path":   fullPath,
		"Url":    "leanote://file/getImage?fileId=" + fileID,
	}, nil
}

func (fs *FileService) GetDirtyImages() ([]*models.Image, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, err
	}
	return fs.db.GetDirtyImages(user.ID)
}

func (fs *FileService) GetDirtyAttachs() ([]*models.Attach, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, err
	}
	return fs.db.GetDirtyAttachs(user.ID)
}

func (fs *FileService) UpdateImageServerID(fileID, serverFileID string) error {
	return fs.db.UpdateImageServerID(fileID, serverFileID)
}

func (fs *FileService) UpdateAttachServerID(fileID, serverFileID string) error {
	return fs.db.UpdateAttachServerID(fileID, serverFileID)
}

func (fs *FileService) GetFileBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (fs *FileService) GetFileMD5(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return utils.MD5(string(data)), nil
}

func (fs *FileService) ExportNoteContent(noteID, format string) (string, error) {
	note, err := fs.db.GetNote(noteID)
	if err != nil || note == nil {
		return "", fmt.Errorf("note not found: %s", noteID)
	}

	user, _ := fs.db.GetActiveUser()
	exportDir := fs.dataDir
	if user != nil {
		exportDir = filepath.Join(fs.GetUserDir(user.ID), "export")
	}
	os.MkdirAll(exportDir, 0755)

	filename := noteID + "." + format
	fullPath := filepath.Join(exportDir, filename)

	content := note.Content
	if note.IsMarkdown && format == "html" {
		content = "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>" +
			note.Title + "</title></head><body><pre>" + content + "</pre></body></html>"
	} else if format == "html" {
		content = "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>" +
			note.Title + "</title></head><body>" + content + "</body></html>"
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", err
	}

	return fullPath, nil
}

func (fs *FileService) GetAllUsers() ([]map[string]interface{}, error) {
	users, err := fs.db.GetAllUsers()
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, len(users))
	for i, u := range users {
		stats := fs.GetDataStats(u.ID)
		result[i] = map[string]interface{}{
			"UserId":      u.ID,
			"Username":    u.Username,
			"Email":       u.Email,
			"Host":        u.Host,
			"IsActive":    u.IsActive,
			"IsLocal":     u.IsLocal,
			"LastSyncUsn": u.LastSyncUsn,
			"DataStats":   stats,
		}
	}
	return result, nil
}

func (fs *FileService) SwitchUser(userID string) error {
	return fs.db.SwitchUser(userID)
}

func (fs *FileService) DeleteUser(userID string) error {
	fs.DeleteUserDir(userID)
	return fs.db.DeleteUser(userID)
}

func (fs *FileService) CreateLocalAccount(username string) (map[string]interface{}, error) {
	userID := utils.ObjectId()
	now := time.Now()

	user := &models.User{
		ID:          userID,
		Username:    username,
		IsActive:    true,
		IsLocal:     true,
		CreatedTime: &now,
	}

	if err := fs.db.InsertUser(user); err != nil {
		return nil, err
	}

	fs.db.SetCurrentUser(userID)

	fs.InitUserDirs(userID)

	defaultNb := &models.Notebook{
		ID:         utils.ObjectId(),
		NotebookID: utils.ObjectId(),
		Title:      "My Notebook",
		UserID:     userID,
		Seq:        0,
	}
	fs.db.InsertNotebook(defaultNb)

	defaultNote := &models.Note{
		ID:          utils.ObjectId(),
		NoteID:      utils.ObjectId(),
		NotebookID:  defaultNb.NotebookID,
		UserID:      userID,
		Title:       "Welcome to Gemsnote",
		Content:     "# Welcome to Gemsnote\n\nThis is your first note. Start writing!",
		IsMarkdown:  true,
		IsDirty:     true,
		LocalIsNew:  true,
		CreatedTime: &now,
		UpdatedTime: &now,
	}
	fs.db.InsertNote(defaultNote)
	fs.db.CountNotes(defaultNb.NotebookID)

	return map[string]interface{}{
		"UserId":          userID,
		"Username":        username,
		"IsLocal":         true,
		"DefaultNotebook": defaultNb.NotebookID,
		"DefaultNote":     defaultNote.NoteID,
	}, nil
}

func (fs *FileService) GetConfig(key string) (string, error) {
	return fs.db.GetConfig(key)
}

func (fs *FileService) SetConfig(key, value string) error {
	return fs.db.SetConfig(key, value)
}

func (fs *FileService) AddNoteHistory(noteID, content string) error {
	return fs.db.AddNoteHistory(noteID, content)
}

func (fs *FileService) GetNoteHistories(noteID string) ([]map[string]interface{}, error) {
	histories, err := fs.db.GetNoteHistories(noteID)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, len(histories))
	for i, h := range histories {
		result[i] = map[string]interface{}{
			"Id":          h.ID,
			"NoteId":      h.NoteID,
			"Content":     h.Content,
			"UpdatedTime": h.UpdatedTime,
		}
	}
	return result, nil
}

func (fs *FileService) CopyNote(noteID, targetNotebookID string) (map[string]interface{}, error) {
	newNote, err := fs.db.CopyNote(noteID, targetNotebookID)
	if err != nil {
		return nil, err
	}
	fs.db.CountNotes(targetNotebookID)
	return map[string]interface{}{
		"NoteId":     newNote.NoteID,
		"NotebookId": newNote.NotebookID,
		"Title":      newNote.Title,
	}, nil
}

func (fs *FileService) ClearTrash() error {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return err
	}

	notes, err := fs.db.GetTrashNotes(user.ID)
	if err != nil {
		return err
	}

	for _, note := range notes {
		fs.DeleteNoteFiles(note.NoteID)
	}

	return fs.db.ClearTrash(user.ID)
}

func (fs *FileService) DeleteNoteFiles(noteID string) {
	attachs, err := fs.db.GetAttachsByNote(noteID)
	if err == nil {
		for _, att := range attachs {
			if att.Path != "" {
				os.Remove(att.Path)
			}
			fs.db.DeleteAttach(att.FileID)
		}
	}
}

func (fs *FileService) ToJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// WriteBase64 writes base64 data to file
func (fs *FileService) WriteBase64(data string, isImage bool, fileType string, title string) (map[string]interface{}, error) {
	user, err := fs.db.GetActiveUser()
	if err != nil || user == nil {
		return nil, fmt.Errorf("no active user")
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}

	fileID := utils.ObjectId()
	filename := fileID + "." + fileType

	var fullPath string
	var dir string
	if isImage {
		dir = fs.GetUserImageDir(user.ID)
		fullPath = filepath.Join(dir, filename)
	} else {
		dir = fs.GetUserAttachDir(user.ID)
		fullPath = filepath.Join(dir, filename)
	}

	if err := os.WriteFile(fullPath, decoded, 0644); err != nil {
		return nil, fmt.Errorf("write file failed: %w", err)
	}

	now := time.Now()
	if isImage {
		img := &models.Image{
			ID:          utils.ObjectId(),
			FileID:      fileID,
			UserID:      user.ID,
			Path:        fullPath,
			IsDirty:     true,
			CreatedTime: &now,
		}
		fs.db.InsertImage(img)
		return map[string]interface{}{
			"FileId":  fileID,
			"Path":    fullPath,
			"Url":     "leanote://file/getImage?fileId=" + fileID,
			"IsImage": true,
		}, nil
	}

	att := &models.Attach{
		ID:          utils.ObjectId(),
		FileID:      fileID,
		UserID:      user.ID,
		Title:       title,
		Type:        fileType,
		Path:        fullPath,
		IsAttach:    true,
		IsDirty:     true,
		CreatedTime: &now,
	}
	fs.db.InsertAttach(att)
	return map[string]interface{}{
		"FileId":   fileID,
		"Path":     fullPath,
		"Title":    title,
		"Type":     fileType,
		"IsAttach": true,
	}, nil
}

// CopyOtherSiteImage downloads external image and registers it
func (fs *FileService) CopyOtherSiteImage(url string) (map[string]interface{}, error) {
	result, err := fs.DownloadImage(url)
	if err != nil {
		return nil, err
	}

	fileID, _ := result["FileId"].(string)
	path, _ := result["Path"].(string)

	img, _ := fs.db.GetImage(fileID)
	if img != nil {
		img.IsDirty = false
		img.ServerFileID = fileID
		fs.db.UpdateImageServerID(fileID, fileID)
	}

	return map[string]interface{}{
		"FileId": fileID,
		"Path":   path,
		"Url":    "leanote://file/getImage?fileId=" + fileID,
	}, nil
}
