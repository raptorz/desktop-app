package webapi

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/models"
	"github.com/gemsnote/gemsnote/service"
)

func TestSharedNotebooksRestoresAndRenewsSession(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/auth/login":
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
