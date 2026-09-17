package webapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	gemsnoteapi "github.com/gemsnote/gemsnote/api"
	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/service"
	"github.com/gemsnote/gemsnote/utils"
)

type ServerProxy struct {
	DB    *db.Database
	Files *service.FileService

	client           *http.Client
	email            string
	pwd              string
	sessionOk        bool
	browserSessionOk bool
	token            string
	remoteUser       *models.User
	versionNotice    string
}

func NewServerProxy(database *db.Database, files *service.FileService) *ServerProxy {
	jar, _ := cookiejar.New(nil)
	p := &ServerProxy{
		DB:    database,
		Files: files,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Jar:     jar,
		},
	}
	p.email, _ = database.GetConfig("proxy:email")
	p.pwd, _ = database.GetConfig("proxy:pwd")
	return p
}

func (p *ServerProxy) host() string {
	host, _ := p.DB.GetConfig("host")
	return strings.TrimRight(host, "/")
}

func (p *ServerProxy) SetHost(host string) {
	if host == "" {
		return
	}
	p.DB.SetConfig("host", strings.TrimRight(host, "/"))
	p.sessionOk = false
	p.browserSessionOk = false
	p.token = ""
	p.remoteUser = nil
}

func (p *ServerProxy) configured() bool {
	return p.host() != ""
}

func (p *ServerProxy) call(method, path string, form url.Values, file *multipart.FileHeader) ([]byte, string, int, error) {
	req, err := http.NewRequest(method, p.host()+path, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if p.token != "" {
		q := req.URL.Query()
		q.Set("token", p.token)
		req.URL.RawQuery = q.Encode()
	}
	if file != nil {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if form != nil {
			for key, vals := range form {
				for _, v := range vals {
					writer.WriteField(key, v)
				}
			}
		}
		src, err := file.Open()
		if err != nil {
			return nil, "", 0, err
		}
		part, err := writer.CreateFormFile("file", filepath.Base(file.Filename))
		if err != nil {
			src.Close()
			return nil, "", 0, err
		}
		if _, err := io.Copy(part, src); err != nil {
			src.Close()
			return nil, "", 0, err
		}
		src.Close()
		writer.Close()
		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	} else if form != nil && method == http.MethodGet {
		query := req.URL.Query()
		for key, values := range form {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		req.URL.RawQuery = query.Encode()
	} else if form != nil {
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.ContentLength = int64(len(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, err
}

func (p *ServerProxy) callJSON(method, path string, payload any) ([]byte, string, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", 0, err
	}
	req, err := http.NewRequest(method, p.host()+path, bytes.NewReader(body))
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if p.token != "" {
		q := req.URL.Query()
		q.Set("token", p.token)
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, err
}

func parseRe(data []byte) (ok bool, msg string, parsed bool) {
	var re struct {
		Ok  *bool `json:"Ok"`
		Msg string
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("true")) {
		return true, "", true
	}
	if bytes.Equal(trimmed, []byte("false")) {
		return false, "", true
	}
	if err := json.Unmarshal(trimmed, &re); err != nil {
		return false, "", false
	}
	if re.Ok == nil {
		return false, "", false
	}
	return *re.Ok, re.Msg, true
}

func isNotLogin(data []byte) bool {
	var re struct {
		Msg string
	}
	if err := json.Unmarshal(bytes.TrimSpace(data), &re); err == nil {
		return re.Msg == "NOTLOGIN"
	}
	return false
}

func (p *ServerProxy) LoginServer(email, pwd string) (bool, string) {
	if !p.configured() {
		return false, "offline"
	}
	data, _, _, err := p.callJSON(http.MethodPost, "/api2/auth/login", map[string]string{"email": email, "pwd": pwd})
	if err != nil {
		return false, "offline"
	}
	ok, msg, parsed := parseRe(data)
	if !parsed {
		return false, "serverError"
	}
	if !ok {
		return false, msg
	}
	var auth struct {
		Token    string `json:"Token"`
		UserID   string `json:"UserId"`
		Username string `json:"Username"`
		Email    string `json:"Email"`
	}
	_ = json.Unmarshal(data, &auth)
	p.email, p.pwd, p.token, p.sessionOk = email, pwd, auth.Token, true
	p.browserSessionOk = false
	if auth.UserID != "" {
		p.remoteUser = &models.User{ID: auth.UserID, Username: auth.Username, Email: auth.Email, IsActive: true}
	}
	p.versionNotice = p.checkServerVersion()
	return true, ""
}

// checkServerVersion distinguishes Gemsnote from an older Leanote endpoint.
// An empty notice means the server could not be checked; login remains usable.
func (p *ServerProxy) checkServerVersion() string {
	resp, err := p.client.Get(p.host() + "/api2/system/version")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "serverMigrationRequired"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var info gemsnoteapi.ServerVersion
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ""
	}
	return gemsnoteapi.ServerVersionNotice(&info, nil)
}

func (p *ServerProxy) VersionNotice() string { return p.versionNotice }

// fetchHistories retrieves the split history API used by Gemsnote. The bool
// reports whether the remote response was understood; callers should fall
// back to the local cache when it is false (for example on old Leanote).
func (p *ServerProxy) fetchHistories(noteID string) ([]map[string]any, bool) {
	if !p.configured() || !p.ensureSession() {
		return nil, false
	}
	data, _, _, err := p.call(http.MethodGet, "/api2/note/getHistories", url.Values{"noteId": {noteID}}, nil)
	if err != nil {
		return nil, false
	}
	var response struct {
		Ok   bool                 `json:"Ok"`
		Item []models.HistoryMeta `json:"Item"`
	}
	if err := json.Unmarshal(data, &response); err != nil || !response.Ok {
		return nil, false
	}

	items := make([]map[string]any, 0, len(response.Item))
	for _, meta := range response.Item {
		contentParams := url.Values{"noteId": {noteID}}
		if meta.ID != "" {
			contentParams.Set("historyId", meta.ID)
		} else {
			// Old Leanote/Gemsnote responses only have an array index.
			contentParams.Set("index", strconv.Itoa(meta.Index))
		}
		contentData, _, _, err := p.call(http.MethodGet, "/api2/note/getHistoryContent", contentParams, nil)
		if err != nil {
			return nil, false
		}
		var contentResponse struct {
			Ok   bool                 `json:"Ok"`
			Item *models.HistoryEntry `json:"Item"`
		}
		if err := json.Unmarshal(contentData, &contentResponse); err != nil || !contentResponse.Ok || contentResponse.Item == nil {
			return nil, false
		}
		items = append(items, map[string]any{
			"Id":          meta.ID,
			"UpdatedTime": timeOrNow(&contentResponse.Item.UpdatedTime),
			"Content":     normalizeContent(contentResponse.Item.Content),
		})
	}
	return items, true
}

func (p *ServerProxy) FetchAPIToken(email, pwd string) string {
	if !p.configured() {
		return ""
	}
	data, _, _, err := p.callJSON(http.MethodPost, "/api2/auth/login", map[string]string{"email": email, "pwd": pwd})
	if err != nil {
		return ""
	}
	var payload struct {
		Ok    bool
		Token string
	}
	if json.Unmarshal(data, &payload) != nil || !payload.Ok {
		return ""
	}
	return payload.Token
}

func (p *ServerProxy) fetchServerUser() *models.User {
	// API2 token authentication does not create a browser session. Fetch the
	// profile through the token-authenticated endpoint instead of Bootstrap.
	if !p.ensureSession() {
		return nil
	}
	data, _, _, err := p.call(http.MethodGet, "/api2/user/info", nil, nil)
	if err != nil {
		return nil
	}
	var payload struct {
		UserId   string
		Username string
		Email    string
		Logo     string
	}
	if json.Unmarshal(data, &payload) != nil || payload.UserId == "" {
		return nil
	}
	if !p.cacheAvatar(payload.UserId, payload.Logo) {
		return nil
	}
	return &models.User{
		ID:       payload.UserId,
		Username: payload.Username,
		Email:    payload.Email,
		IsActive: true,
	}
}

func (p *ServerProxy) cacheAvatar(userID, logo string) bool {
	logo = strings.TrimSpace(logo)
	if logo == "" {
		p.DB.SetConfig("logo:"+userID, "")
		p.DB.SetConfig("logo_source:"+userID, "")
		return true
	}
	active, _ := p.DB.GetActiveUser()
	if active == nil || active.ID != userID {
		return true // login has not yet adopted this account locally
	}
	if source, _ := p.DB.GetConfig("logo_source:" + userID); source == logo {
		if cached, _ := p.DB.GetConfig("logo:" + userID); cached != "" {
			if strings.HasPrefix(cached, "/api2/file/getImage?fileId=") {
				fileID := strings.TrimPrefix(cached, "/api2/file/getImage?fileId=")
				if path, err := p.Files.GetImage(fileID); err == nil && path != "" {
					if _, err := os.Stat(path); err == nil {
						return true
					}
				}
			} else {
				return true
			}
		}
	}
	path := strings.TrimLeft(logo, "/")
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// A remote URL is usable online, but only same-origin paths are cached.
		p.DB.SetConfig("logo:"+userID, logo)
		return true
	}
	if !strings.HasPrefix(path, "public/upload/") {
		return false
	}
	resp, err := p.client.Get(p.host() + "/" + path)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return false
	}
	dir := p.Files.GetUserImageDir(userID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	file, err := os.CreateTemp(dir, "avatar-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_, copyErr := io.Copy(file, io.LimitReader(resp.Body, 10<<20))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(name)
		return false
	}
	fileID := utils.ObjectId()
	if err := p.Files.AddImageForce(fileID, name); err != nil {
		os.Remove(name)
		return false
	}
	p.DB.SetConfig("logo:"+userID, "/api2/file/getImage?fileId="+fileID)
	p.DB.SetConfig("logo_source:"+userID, logo)
	return true
}

// RefreshUserProfile refreshes profile metadata cached by the local bridge.
// In particular, avatar URLs are part of bootstrap rather than sync payloads.
func (p *ServerProxy) RefreshUserProfile() bool {
	return p.fetchServerUser() != nil
}

func (p *ServerProxy) ensureSession() bool {
	ok, _ := p.ensureSessionStatus()
	return ok
}

func (p *ServerProxy) ensureBrowserSessionStatus() (bool, string) {
	if ok, msg := p.ensureSessionStatus(); !ok {
		return false, msg
	}
	if p.browserSessionOk {
		return true, ""
	}
	data, _, _, err := p.callJSON(http.MethodPost, "/api2/auth/session", map[string]string{"email": p.email, "pwd": p.pwd})
	if err != nil {
		return false, "offline"
	}
	ok, msg, parsed := parseRe(data)
	if !parsed {
		return false, "serverError"
	}
	p.browserSessionOk = ok
	return ok, msg
}

func (p *ServerProxy) ensureSessionStatus() (bool, string) {
	if p.sessionOk {
		return true, ""
	}
	if p.email == "" || p.pwd == "" {
		return false, "NOTLOGIN"
	}
	ok, msg := p.LoginServer(p.email, p.pwd)
	return ok, msg
}

func adminCacheKey(host, userID string) string {
	return "proxy:is_admin:" + db.SharedAccountID(host, userID)
}

func (p *ServerProxy) CachedIsAdmin(user *models.User) bool {
	if user == nil {
		return false
	}
	value, _ := p.DB.GetConfig(adminCacheKey(user.Host, user.ID))
	admin, _ := strconv.ParseBool(value)
	return admin
}

func (p *ServerProxy) GuestConfig() (openRegister, needCaptcha bool) {
	if !p.configured() {
		return false, false
	}
	data, _, _, err := p.call(http.MethodGet, "/api2/web/bootstrap", nil, nil)
	if err != nil {
		return false, false
	}
	var payload struct {
		OpenRegister bool
		NeedCaptcha  bool
	}
	if json.Unmarshal(data, &payload) == nil {
		return payload.OpenRegister, payload.NeedCaptcha
	}
	return false, false
}

func (p *ServerProxy) SharedNotebooks(user *models.User) (map[string]any, bool) {
	empty := map[string]any{}
	if !p.configured() {
		return empty, false
	}
	if ok, _ := p.ensureBrowserSessionStatus(); !ok {
		return empty, false
	}
	shared, isAdmin, ok := p.fetchBootstrapSession()
	if !ok {
		p.browserSessionOk = false
		if sessionReady, _ := p.ensureBrowserSessionStatus(); sessionReady {
			shared, isAdmin, ok = p.fetchBootstrapSession()
		}
	}
	if !ok {
		return empty, false
	}
	if user != nil {
		p.DB.SetConfig(adminCacheKey(user.Host, user.ID), strconv.FormatBool(isAdmin))
	}
	return shared, isAdmin
}

func (p *ServerProxy) fetchBootstrapSession() (map[string]any, bool, bool) {
	data, _, _, err := p.call(http.MethodGet, "/api2/web/bootstrap", nil, nil)
	if err != nil {
		return nil, false, false
	}
	var payload struct {
		SharedNotebooks map[string]any
		IsAdmin         bool
		User            *struct{ UserId string }
	}
	if json.Unmarshal(data, &payload) != nil || payload.User == nil {
		return nil, false, false
	}
	if payload.SharedNotebooks == nil {
		if isNotLogin(data) {
			return nil, false, false
		}
		payload.SharedNotebooks = map[string]any{}
	}
	return payload.SharedNotebooks, payload.IsAdmin, true
}

func (p *ServerProxy) Logout() {
	// Logout is deliberately local. The desktop session cookie is private to
	// this client, so replacing its jar invalidates it without making logout
	// depend on the remote server being reachable.
	jar, _ := cookiejar.New(nil)
	p.client.Jar = jar
	p.sessionOk = false
	p.browserSessionOk = false
	p.token = ""
	p.remoteUser = nil
	// Credentials are only a session aid for reconnecting while logged in.
	// Keeping them after logout would let a later proxied request silently
	// authenticate again without an explicit login.
	p.email, p.pwd = "", ""
	p.DB.SetConfig("proxy:email", "")
	p.DB.SetConfig("proxy:pwd", "")
}

func (p *ServerProxy) Forward(w http.ResponseWriter, r *http.Request, form url.Values, file *multipart.FileHeader) bool {
	if !p.configured() {
		return false
	}
	var rawJSON json.RawMessage
	if file == nil && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var err error
		rawJSON, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, `{"Ok":false,"Msg":"invalidJSON"}`, http.StatusBadRequest)
			return true
		}
	}
	send := func() ([]byte, string, int, error) {
		if rawJSON != nil {
			return p.callJSON(r.Method, r.URL.RequestURI(), rawJSON)
		}
		return p.call(r.Method, r.URL.RequestURI(), form, file)
	}
	data, contentType, status, err := send()
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"Ok":false,"Msg":"offline"}`))
		return true
	}
	if needsBrowserSession(r.URL.Path) && isNotLogin(data) {
		p.browserSessionOk = false
		if ok, msg := p.ensureBrowserSessionStatus(); !ok {
			if msg == "" {
				msg = "NOTLOGIN"
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"Ok": false, "Msg": msg})
			return true
		}
		data, contentType, status, err = send()
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"Ok":false,"Msg":"offline"}`))
			return true
		}
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if status > 0 {
		w.WriteHeader(status)
	}
	w.Write(data)
	return true
}

func needsBrowserSession(path string) bool {
	return path == "/api2/groups" || path == "/api2/web/groups" || path == "/api2/admin/data" || path == "/api2/avatar" || path == "/api2/file/uploadAvatar" || path == "/api2/user/updateUsername" || path == "/api2/user/updatePwd" || path == "/api2/user/reSendActiveEmail" || strings.HasPrefix(path, "/api2/member/") || strings.HasPrefix(path, "/api2/share/") || strings.HasPrefix(path, "/api2/web/")
}

var guestPaths = []string{"/captcha/", "/api2/auth/register", "/api2/auth/password/request", "/api2/auth/password/reset", "/api2/web/verifyEmail"}

func (p *ServerProxy) isGuestPath(path string) bool {
	for _, prefix := range guestPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (h *Handler) routeProxied(w http.ResponseWriter, r *http.Request) bool {
	if h.Proxy == nil {
		return false
	}
	path := r.URL.Path
	proxy := h.Proxy

	proxied := strings.HasPrefix(path, "/api2/share/") || strings.HasPrefix(path, "/api2/member/") ||
		strings.HasPrefix(path, "/api2/user/") || strings.HasPrefix(path, "/captcha/") ||
		path == "/api2/groups" || path == "/api2/admin/data" || path == "/api2/avatar" ||
		path == "/api2/web/shareMembers" || path == "/api2/web/groups" || path == "/api2/web/emailChange" ||
		path == "/api2/web/verifyEmail" || path == "/api2/auth/register" || path == "/api2/auth/password/request" ||
		path == "/api2/auth/password/reset" || path == "/api2/auth/login" || path == "/api2/file/uploadAvatar" ||
		strings.HasPrefix(path, "/api2/web/admin") || path == "/api2/admin/data"
	if !proxied {
		return false
	}

	if path == "/api2/auth/login" || path == "/api2/auth/register" || path == "/api2/auth/password/request" || path == "/api2/auth/password/reset" {
		if host := h.form(r, "host"); host != "" {
			proxy.SetHost(host)
		}
	}

	switch {
	case path == "/captcha/":
		return proxy.Forward(w, r, nil, nil)
	case proxy.isGuestPath(path):
		return proxy.Forward(w, r, r.Form, nil)
	case path == "/api2/file/uploadAvatar" || path == "/api2/avatar":
		_, fh, err := r.FormFile("file")
		if err != nil {
			h.fail(w, "noFile")
			return true
		}
		return h.proxyAvatar(w, r, fh)
	case path == "/api2/user/updatePwd":
		return h.proxyUpdatePwd(w, r)
	}

	if ok, msg := proxy.ensureSessionStatus(); !ok {
		if msg == "" {
			msg = "NOTLOGIN"
		}
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": msg})
		return true
	}
	if needsBrowserSession(path) {
		if ok, msg := proxy.ensureBrowserSessionStatus(); !ok {
			if msg == "" {
				msg = "NOTLOGIN"
			}
			h.writeJSON(w, map[string]any{"Ok": false, "Msg": msg})
			return true
		}
	}
	return proxy.Forward(w, r, r.Form, nil)
}

func (h *Handler) proxyUpdatePwd(w http.ResponseWriter, r *http.Request) bool {
	user := h.activeUser()
	if user == nil {
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": "NOTLOGIN"})
		return true
	}
	newPwd := r.FormValue("pwd")
	if ok, msg := h.Proxy.ensureBrowserSessionStatus(); !ok {
		h.fail(w, msg)
		return true
	}
	data, _, status, err := h.Proxy.call(r.Method, r.URL.RequestURI(), r.Form, nil)
	if err != nil {
		h.fail(w, "offline")
		return true
	}
	ok, msg, parsed := parseRe(data)
	if status < 200 || status >= 300 || !parsed || !ok {
		if msg == "" {
			msg = "updatePwdFailed"
		}
		h.fail(w, msg)
		return true
	}
	if err := h.DB.UpdateUserPwd(user.ID, utils.MD5WithSalt(newPwd, user.ID)); err != nil {
		h.fail(w, err.Error())
		return true
	}
	h.DB.SetConfig("proxy:pwd", newPwd)
	h.Proxy.pwd = newPwd
	h.Proxy.sessionOk = false
	h.Proxy.browserSessionOk = false
	h.Proxy.token = ""
	h.ok(w)
	return true
}

func (h *Handler) proxyAvatar(w http.ResponseWriter, r *http.Request, fh *multipart.FileHeader) bool {
	user := h.activeUser()
	if user == nil {
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": "NOTLOGIN"})
		return true
	}
	if ok, msg := h.Proxy.ensureBrowserSessionStatus(); !ok {
		if msg == "" {
			msg = "NOTLOGIN"
		}
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": msg})
		return true
	}
	response, _, _, err := h.Proxy.call(r.Method, r.URL.RequestURI(), r.Form, fh)
	if err != nil {
		h.fail(w, "offline")
		return true
	}
	ok, msg, parsed := parseRe(response)
	if !parsed {
		h.fail(w, "serverError")
		return true
	}
	if !ok {
		h.fail(w, msg)
		return true
	}
	tmp, err := saveMultipartFile(fh, os.TempDir())
	if err != nil {
		h.fail(w, err.Error())
		return true
	}
	defer os.Remove(tmp)
	result, err := h.Files.CopyFile(tmp, true)
	if err != nil {
		h.fail(w, err.Error())
		return true
	}
	fileID, _ := result["FileId"].(string)
	if fileID == "" {
		h.fail(w, "avatarCacheFailed")
		return true
	}
	h.DB.SetConfig("logo:"+user.ID, "/api2/file/getImage?fileId="+fileID)
	h.writeJSON(w, map[string]any{"Ok": true, "Id": fileID})
	return true
}
