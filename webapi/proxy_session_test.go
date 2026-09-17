package webapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/service"
	"github.com/gemsnote/gemsnote/utils"
)

func TestSharedNotebooksRestoresAndRenewsSession(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
			w.Write([]byte(`{"Ok":true,"Token":"token","UserId":"admin"}`))
		case "/api2/auth/session":
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
			w.Write([]byte(`{"Ok":true}`))
		case "/api2/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0","min_version":""}`))
		case "/api2/web/bootstrap":
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "valid" {
				w.Write([]byte(`{"Ok":true,"User":null}`))
				return
			}
			w.Write([]byte(`{"Ok":true,"User":{"UserId":"admin"},"SharedNotebooks":{},"IsAdmin":true}`))
		}
	}))
	defer server.Close()

	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetConfig("host", server.URL)
	database.SetConfig("proxy:email", "admin")
	database.SetConfig("proxy:pwd", "secret")
	proxy := NewServerProxy(database, service.NewFileService(database))

	_, admin := proxy.SharedNotebooks(nil)
	if !admin || logins.Load() != 1 {
		t.Fatalf("initial session: admin=%v logins=%d", admin, logins.Load())
	}
	proxy.client.Jar, _ = newEmptyCookieJar()
	_, admin = proxy.SharedNotebooks(nil)
	if !admin || logins.Load() != 2 {
		t.Fatalf("renewed session: admin=%v logins=%d", admin, logins.Load())
	}
}

func TestAccountGroupsUsesBrowserSession(t *testing.T) {
	var tokenLogins, sessionLogins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
			tokenLogins.Add(1)
			w.Write([]byte(`{"Ok":true,"Token":"token","UserId":"user1"}`))
		case "/api2/auth/session":
			sessionLogins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
			w.Write([]byte(`{"Ok":true}`))
		case "/api2/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0"}`))
		case "/api2/groups":
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "valid" {
				w.Write([]byte(`{"Ok":false,"Msg":"NOTLOGIN"}`))
				return
			}
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	e := newTestEnv(t)
	e.db.SetConfig("host", server.URL)
	e.db.SetConfig("proxy:email", "tester")
	e.db.SetConfig("proxy:pwd", "secret")
	e.handler.Proxy = NewServerProxy(e.db, e.handler.Files)
	_, body := e.get(t, "/api2/groups")
	if string(body) != "[]" || tokenLogins.Load() != 1 || sessionLogins.Load() != 1 {
		t.Fatalf("account groups response=%s token logins=%d session logins=%d", body, tokenLogins.Load(), sessionLogins.Load())
	}
}

func TestAccountUpdateUsesBrowserSessionAndPreservesJSON(t *testing.T) {
	var sessionLogins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
			w.Write([]byte(`{"Ok":true,"Token":"token","UserId":"user1"}`))
		case "/api2/auth/session":
			sessionLogins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
			w.Write([]byte(`{"Ok":true}`))
		case "/api2/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0"}`))
		case "/api2/user/updateUsername":
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "valid" {
				w.Write([]byte(`{"Ok":false,"Msg":"NOTLOGIN"}`))
				return
			}
			var payload struct {
				Username string
				Count    int
			}
			if r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Username != "renamed" || payload.Count != 2 {
				w.Write([]byte(`{"Ok":false,"Msg":"badRequest"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"Ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	e := newTestEnv(t)
	e.db.SetConfig("host", server.URL)
	e.db.SetConfig("proxy:email", "tester")
	e.db.SetConfig("proxy:pwd", "secret")
	e.handler.Proxy = NewServerProxy(e.db, e.handler.Files)
	req := httptest.NewRequest(http.MethodPost, "/api2/user/updateUsername", bytes.NewBufferString(`{"username":"renamed","count":2}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || rec.Body.String() != `{"Ok":true}` || sessionLogins.Load() != 1 {
		t.Fatalf("account JSON forward: status=%d body=%s sessions=%d", rec.Code, rec.Body.String(), sessionLogins.Load())
	}
}

func TestUpdatePasswordCachesOnlyConfirmedRemoteChange(t *testing.T) {
	e := newTestEnv(t)
	userID, _ := e.login(t)
	var allow atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
			w.Write([]byte(`{"Ok":true,"Token":"token","UserId":"` + userID + `"}`))
		case "/api2/auth/session":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
			w.Write([]byte(`{"Ok":true}`))
		case "/api2/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0"}`))
		case "/api2/user/updatePwd":
			if _, err := r.Cookie("session"); err != nil {
				w.Write([]byte(`{"Ok":false,"Msg":"NOTLOGIN"}`))
				return
			}
			if !allow.Load() {
				w.Write([]byte(`{"Ok":false,"Msg":"wrongPassword"}`))
				return
			}
			w.Write([]byte(`{"Ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	e.db.SetConfig("host", server.URL)
	e.db.SetConfig("proxy:email", "tester")
	e.db.SetConfig("proxy:pwd", "secret")
	e.handler.Proxy = NewServerProxy(e.db, e.handler.Files)
	_, body := e.post(t, "/api2/user/updatePwd", url.Values{"oldPwd": {"wrong"}, "pwd": {"new-secret"}})
	if !bytes.Contains(body, []byte("wrongPassword")) {
		t.Fatalf("rejected password response: %s", body)
	}
	user, _ := e.db.GetUser(userID)
	if user.Pwd != utils.MD5WithSalt("secret", userID) {
		t.Fatalf("local password changed after remote rejection")
	}
	allow.Store(true)
	_, body = e.post(t, "/api2/user/updatePwd", url.Values{"oldPwd": {"secret"}, "pwd": {"new-secret"}})
	if !bytes.Contains(body, []byte(`"Ok":true`)) {
		t.Fatalf("accepted password response: %s", body)
	}
	user, _ = e.db.GetUser(userID)
	if user.Pwd != utils.MD5WithSalt("new-secret", userID) {
		t.Fatalf("local password not updated after remote acceptance")
	}
}

func TestAvatarUploadForwardsAjaxHeader(t *testing.T) {
	e := newTestEnv(t)
	userID, _ := e.login(t)
	var receivedUpload atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
			w.Write([]byte(`{"Ok":true,"Token":"token","UserId":"` + userID + `"}`))
		case "/api2/auth/session":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid", Path: "/"})
			w.Write([]byte(`{"Ok":true}`))
		case "/api2/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0"}`))
		case "/api2/avatar":
			if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				w.Write([]byte(`{"Ok":false,"Msg":"invalidRequest"}`))
				return
			}
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "valid" {
				w.Write([]byte(`{"Ok":false,"Msg":"NOTLOGIN"}`))
				return
			}
			if file, _, err := r.FormFile("file"); err == nil {
				file.Close()
				receivedUpload.Store(true)
			}
			w.Write([]byte(`{"Ok":true,"Id":"0123456789abcdef01234567"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	e.db.SetConfig("host", server.URL)
	e.db.SetConfig("proxy:email", "tester")
	e.db.SetConfig("proxy:pwd", "secret")
	e.handler.Proxy = NewServerProxy(e.db, e.handler.Files)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("avatar image")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api2/avatar", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	var result struct {
		Ok bool
		Id string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !receivedUpload.Load() || !result.Ok || result.Id == "" || result.Id == "0123456789abcdef01234567" {
		t.Fatalf("avatar upload was not forwarded successfully: %s", rec.Body.String())
	}
	logo, _ := e.db.GetConfig("logo:" + userID)
	if logo != "/api2/file/getImage?fileId="+result.Id {
		t.Fatalf("uploaded avatar is not available from the local image route: %q", logo)
	}
}

func TestProfileAvatarUsesTokenAndCachesImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
			w.Write([]byte(`{"Ok":true,"Token":"test-token","UserId":"user1"}`))
		case "/api2/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0"}`))
		case "/api2/user/info":
			if r.URL.Query().Get("token") != "test-token" {
				w.Write([]byte(`{"Ok":false,"Msg":"NOTLOGIN"}`))
				return
			}
			w.Write([]byte(`{"UserId":"user1","Username":"tester","Logo":"public/upload/avatar.jpeg"}`))
		case "/public/upload/avatar.jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("image bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	database, err := db.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InsertUser(&models.User{ID: "user1", Username: "tester", Host: server.URL, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	database.SetCurrentUser("user1")
	database.SetConfig("host", server.URL)
	files := service.NewFileService(database)
	files.SetDataDir(t.TempDir())
	proxy := NewServerProxy(database, files)
	if ok, msg := proxy.LoginServer("tester", "secret"); !ok {
		t.Fatalf("login failed: %s", msg)
	}
	if !proxy.RefreshUserProfile() {
		t.Fatal("profile refresh failed")
	}
	logo, _ := database.GetConfig("logo:user1")
	if logo == "" || logo == "public/upload/avatar.jpeg" {
		t.Fatalf("avatar not cached locally: %q", logo)
	}
}

func newEmptyCookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}
