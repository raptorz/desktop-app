package webapi

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/service"
)

func TestSharedNotebooksRestoresAndRenewsSession(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doLogin":
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "valid"})
			w.Write([]byte(`{"Ok":true}`))
		case "/api/system/version":
			w.Write([]byte(`{"server":"gemsnote","version":"1.0.0","min_version":""}`))
		case "/web/bootstrap":
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

func newEmptyCookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}
