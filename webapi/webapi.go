package webapi

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gemsnote/gemsnote/db"
	"github.com/gemsnote/gemsnote/service"
)

const pageSize = 100

type Handler struct {
	DB      *db.Database
	Files   *service.FileService
	Proxy   *ServerProxy
	Version string
	Dist    fs.FS
	OnLogin func()
	// OnLogout is called synchronously before the local session is cleared.
	// Returning an error keeps the session active so local changes are not
	// discarded when the final sync cannot be completed.
	OnLogout func() error
	// OnSync / OnFullSync run a blocking incremental or full sync on behalf of
	// the SPA's account menu (/web/sync, /web/fullSync). The returned value is
	// written as the JSON response; a non-nil error becomes {Ok:false,Msg}.
	OnSync           func() (any, error)
	OnFullSync       func() (any, error)
	OnSharedDownload func()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && !isGetApiPath(r.URL.Path) {
		h.serveApp(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.writeJSON(w, map[string]any{"Ok": false, "Msg": "invalidForm"})
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			h.writeJSON(w, map[string]any{"Ok": false, "Msg": "invalidJSON"})
			return
		}
		if r.Form == nil {
			r.Form = make(url.Values)
		}
		for key, value := range payload {
			switch v := value.(type) {
			case []any:
				for i, item := range v {
					r.Form.Set(fmt.Sprintf("%s[%d]", key, i), fmt.Sprint(item))
				}
			default:
				r.Form.Set(key, fmt.Sprint(v))
			}
		}
	}

	if h.route(w, r) {
		return
	}
	log.Printf("webapi: unhandled %s %s", r.Method, r.URL.Path)
	h.writeJSON(w, map[string]any{"Ok": false, "Msg": "notFound"})
}

// isGetApiPath lists GET endpoints served by the handler; every other GET is an
// SPA route rendered via index.html fallback.
func isGetApiPath(path string) bool {
	for _, prefix := range []string{
		"/api2/bootstrap",
		"/api2/web/bootstrap",
		"/api2/web/sync",
		"/api2/web/fullSync",
		"/api2/captcha/",
		"/captcha/",
		"/api2/attach/download",
		"/api2/attachments/download",
		"/api2/file/getImage",
		"/api2/file/getAttach",
		"/api2/system/version",
		"/api2/user/getSyncState",
		"/api2/notebook/getSyncNotebooks",
		"/api2/note/getSyncNotes",
		"/api2/note/getNoteContent",
		"/api2/note/getNote",
		"/api2/note/getHistories",
		"/api2/note/getHistoryContent",
		"/api2/tag/getSyncTags",
		"/api2/shared/",
		"/api2/logout",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (h *Handler) route(w http.ResponseWriter, r *http.Request) bool {
	path, method := r.URL.Path, r.Method
	if h.rejectSharedWrite(w, r, path) {
		return true
	}

	switch {
	case path == "/api2/bootstrap" && method == http.MethodGet:
		h.bootstrap(w)
	case path == "/api2/notes":
		h.notes(w, r)
	case path == "/api2/star":
		h.star(w, r)
	case path == "/api2/document":
		h.document(w, r)
	case path == "/api2/save":
		h.save(w, r)
	case path == "/api2/restore":
		h.restore(w, r)
	case path == "/api2/attachments":
		h.getAttachs(w, r)
	case path == "/api2/attachments/upload":
		h.uploadAttach(w, r)
	case path == "/api2/attachments/delete":
		h.deleteAttach(w, r)
	case path == "/api2/attachments/download" && method == http.MethodGet:
		h.downloadAttach(w, r)
	case path == "/api2/avatar":
		h.routeProxied(w, r)
	case path == "/api2/web/bootstrap" && method == http.MethodGet:
		h.bootstrap(w)
	case path == "/api2/web/notes":
		h.notes(w, r)
	case path == "/api2/web/star":
		h.star(w, r)
	case path == "/api2/web/sync" && method == http.MethodPost:
		h.syncNow(w, h.OnSync)
	case path == "/api2/web/fullSync" && method == http.MethodPost:
		h.syncNow(w, h.OnFullSync)
	case path == "/api2/web/logout" && method == http.MethodPost:
		h.logoutJSON(w)
	case path == "/api2/logout" && method == http.MethodPost:
		h.logoutJSON(w)
	case path == "/api2/web/document":
		h.document(w, r)
	case path == "/api2/share/listShareNotes":
		h.sharedNotes(w, r)
	case path == "/api2/web/save":
		h.save(w, r)
	case path == "/api2/web/restore":
		h.restore(w, r)
	case path == "/api2/notebook/addNotebook":
		h.addNotebook(w, r)
	case path == "/api2/notebook/updateNotebookTitle":
		h.renameNotebook(w, r)
	case path == "/api2/notebook/deleteNotebook":
		h.deleteNotebook(w, r)
	case path == "/api2/note/deleteNote":
		h.deleteNote(w, r)
	case path == "/api2/note/deleteTrash":
		h.deleteTrashNote(w, r)
	case path == "/api2/note/moveNote":
		h.moveNote(w, r)
	case path == "/api2/note/copyNote":
		h.copyNote(w, r)
	case path == "/api2/noteContentHistory/listHistories":
		h.listHistories(w, r)
	case path == "/api2/attach/getAttachs":
		h.getAttachs(w, r)
	case path == "/api2/attach/uploadAttach":
		h.uploadAttach(w, r)
	case path == "/api2/attach/deleteAttach":
		h.deleteAttach(w, r)
	case path == "/api2/attach/queueSharedDownload":
		h.queueSharedDownload(w, r)
	case path == "/api2/attach/download" && method == http.MethodGet:
		h.downloadAttach(w, r)
	case path == "/api2/file/pasteImage":
		h.pasteImage(w, r)
	case path == "/api2/file/getImage":
		h.serveImage(w, r)
	case path == "/api2/auth/session":
		if host := h.form(r, "host"); host != "" && h.Proxy != nil {
			h.Proxy.SetHost(host)
		}
		h.doLogin(w, r)
	case path == "/api2/logout" && method == http.MethodGet:
		h.logout(w, r)
	default:
		return h.routeProxied(w, r)
	}
	return true
}

func (h *Handler) serveApp(w http.ResponseWriter, r *http.Request) {
	if h.Dist == nil {
		http.NotFound(w, r)
		return
	}
	index, err := fs.ReadFile(h.Dist, "index.html")
	if err != nil {
		http.Error(w, "前端尚未构建，请运行 bash build-frontend.sh", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}

func (h *Handler) writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) ok(w http.ResponseWriter) {
	h.writeJSON(w, map[string]any{"Ok": true})
}

func (h *Handler) fail(w http.ResponseWriter, msg string) {
	h.writeJSON(w, map[string]any{"Ok": false, "Msg": msg})
}

func (h *Handler) form(r *http.Request, key string) string {
	return strings.TrimSpace(r.FormValue(key))
}
