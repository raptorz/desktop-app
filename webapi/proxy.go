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

	client        *http.Client
	email         string
	pwd           string
	sessionOk     bool
	versionNotice string
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
}

func (p *ServerProxy) configured() bool {
	return p.host() != ""
}

func (p *ServerProxy) call(method, path string, form url.Values, file *multipart.FileHeader) ([]byte, string, error) {
	req, err := http.NewRequest(method, p.host()+path, nil)
	if err != nil {
		return nil, "", err
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
			return nil, "", err
		}
		part, err := writer.CreateFormFile("file", filepath.Base(file.Filename))
		if err != nil {
			src.Close()
			return nil, "", err
		}
		if _, err := io.Copy(part, src); err != nil {
			src.Close()
			return nil, "", err
		}
		src.Close()
		writer.Close()
		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
		req.Header.Set("Content-Type", writer.FormDataContentType())
	} else if form != nil {
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.ContentLength = int64(len(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.Header.Get("Content-Type"), err
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
	form := url.Values{"email": {email}, "pwd": {pwd}}
	data, _, err := p.call(http.MethodPost, "/doLogin", form, nil)
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
	p.email, p.pwd, p.sessionOk = email, pwd, true
	p.versionNotice = p.checkServerVersion()
	return true, ""
}

// checkServerVersion distinguishes Gemsnote from an older Leanote endpoint.
// An empty notice means the server could not be checked; login remains usable.
func (p *ServerProxy) checkServerVersion() string {
	resp, err := p.client.Get(p.host() + "/api/system/version")
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

func (p *ServerProxy) FetchAPIToken(email, pwd string) string {
	if !p.configured() {
		return ""
	}
	data, _, err := p.call(http.MethodPost, "/api/auth/login", url.Values{"email": {email}, "pwd": {pwd}}, nil)
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
	data, _, err := p.call(http.MethodGet, "/web/bootstrap", nil, nil)
	if err != nil {
		return nil
	}
	var payload struct {
		User struct {
			UserId   string
			Username string
			Email    string
		}
	}
	if json.Unmarshal(data, &payload) != nil || payload.User.UserId == "" {
		return nil
	}
	return &models.User{
		ID:       payload.User.UserId,
		Username: payload.User.Username,
		Email:    payload.User.Email,
		IsActive: true,
	}
}

func (p *ServerProxy) ensureSession() bool {
	if p.sessionOk {
		return true
	}
	if p.email == "" || p.pwd == "" {
		return false
	}
	ok, _ := p.LoginServer(p.email, p.pwd)
	return ok
}

func (p *ServerProxy) GuestConfig() (openRegister, needCaptcha bool) {
	if !p.configured() {
		return false, false
	}
	data, _, err := p.call(http.MethodGet, "/web/bootstrap", nil, nil)
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
	shared, isAdmin, ok := p.fetchBootstrapSession()
	if !ok && p.ensureSession() {
		shared, isAdmin, ok = p.fetchBootstrapSession()
	}
	if !ok {
		return empty, false
	}
	return shared, isAdmin
}

func (p *ServerProxy) fetchBootstrapSession() (map[string]any, bool, bool) {
	data, _, err := p.call(http.MethodGet, "/web/bootstrap", nil, nil)
	if err != nil {
		return nil, false, false
	}
	var payload struct {
		SharedNotebooks map[string]any
		IsAdmin         bool
	}
	if json.Unmarshal(data, &payload) != nil {
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
	if p.configured() {
		p.call(http.MethodGet, "/logout", nil, nil)
	}
	p.sessionOk = false
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
	data, contentType, err := p.call(r.Method, r.URL.RequestURI(), form, file)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"Ok":false,"Msg":"offline"}`))
		return true
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Write(data)
	return true
}

var guestPaths = []string{"/captcha/", "/doRegister", "/doFindPassword", "/findPasswordUpdate", "/web/verifyEmail"}

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

	proxied := strings.HasPrefix(path, "/share/") || strings.HasPrefix(path, "/member/") ||
		strings.HasPrefix(path, "/user/") || strings.HasPrefix(path, "/captcha/") ||
		path == "/web/shareMembers" || path == "/web/groups" || path == "/web/emailChange" ||
		path == "/web/verifyEmail" || path == "/doRegister" || path == "/doFindPassword" ||
		path == "/findPasswordUpdate" || path == "/file/uploadAvatar" ||
		strings.HasPrefix(path, "/web/admin")
	if !proxied {
		return false
	}

	if path == "/doLogin" || path == "/doRegister" || path == "/doFindPassword" {
		if host := h.form(r, "host"); host != "" {
			proxy.SetHost(host)
		}
	}

	switch {
	case path == "/captcha/":
		return proxy.Forward(w, r, nil, nil)
	case proxy.isGuestPath(path):
		return proxy.Forward(w, r, r.Form, nil)
	case path == "/file/uploadAvatar":
		_, fh, err := r.FormFile("file")
		if err != nil {
			h.fail(w, "noFile")
			return true
		}
		return h.proxyAvatar(w, r, fh)
	case path == "/user/updatePwd":
		return h.proxyUpdatePwd(w, r)
	}

	if !proxy.ensureSession() {
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": "NOTLOGIN"})
		return true
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
	if !h.Proxy.Forward(w, r, r.Form, nil) {
		return true
	}
	if newPwd != "" {
		h.DB.UpdateUserPwd(user.ID, utils.MD5WithSalt(newPwd, user.ID))
		h.DB.SetConfig("proxy:pwd", newPwd)
		h.Proxy.pwd = newPwd
	}
	return true
}

func (h *Handler) proxyAvatar(w http.ResponseWriter, r *http.Request, fh *multipart.FileHeader) bool {
	user := h.activeUser()
	if user == nil {
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": "NOTLOGIN"})
		return true
	}
	if !h.Proxy.Forward(w, r, r.Form, fh) {
		return true
	}
	tmp, err := saveMultipartFile(fh, os.TempDir())
	if err != nil {
		return true
	}
	defer os.Remove(tmp)
	if result, err := h.Files.CopyFile(tmp, true); err == nil {
		fileID, _ := result["FileId"].(string)
		h.DB.SetConfig("logo:"+user.ID, "/file/getImage?fileId="+fileID)
	}
	return true
}
