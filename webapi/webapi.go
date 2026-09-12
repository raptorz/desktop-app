package webapi

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
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
	OnLogout         func() error
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

	if h.route(w, r) {
		return
	}
	log.Printf("webapi: unhandled %s %s", r.Method, r.URL.Path)
	h.writeJSON(w, map[string]any{"Ok": false, "Msg": "notFound"})
}

// isGetApiPath lists GET endpoints served by the handler; every other GET is an
// SPA route rendered via index.html fallback.
func isGetApiPath(path string) bool {
	for _, prefix := range []string{"/web/", "/captcha/", "/attach/download", "/api/", "/file/", "/logout"} {
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
	case path == "/web/bootstrap" && method == http.MethodGet:
		h.bootstrap(w)
	case path == "/web/notes":
		h.notes(w, r)
	case path == "/web/star":
		h.star(w, r)
	case path == "/web/document":
		h.document(w, r)
	case path == "/share/listShareNotes":
		h.sharedNotes(w, r)
	case path == "/web/save":
		h.save(w, r)
	case path == "/web/restore":
		h.restore(w, r)
	case path == "/notebook/addNotebook":
		h.addNotebook(w, r)
	case path == "/notebook/updateNotebookTitle":
		h.renameNotebook(w, r)
	case path == "/notebook/deleteNotebook":
		h.deleteNotebook(w, r)
	case path == "/note/deleteNote":
		h.deleteNote(w, r)
	case path == "/note/deleteTrash":
		h.deleteTrashNote(w, r)
	case path == "/note/moveNote":
		h.moveNote(w, r)
	case path == "/note/copyNote":
		h.copyNote(w, r)
	case path == "/noteContentHistory/listHistories":
		h.listHistories(w, r)
	case path == "/attach/getAttachs":
		h.getAttachs(w, r)
	case path == "/attach/uploadAttach":
		h.uploadAttach(w, r)
	case path == "/attach/deleteAttach":
		h.deleteAttach(w, r)
	case path == "/attach/queueSharedDownload":
		h.queueSharedDownload(w, r)
	case path == "/attach/download" && method == http.MethodGet:
		h.downloadAttach(w, r)
	case path == "/file/pasteImage":
		h.pasteImage(w, r)
	case path == "/api/file/getImage" || path == "/file/getImage":
		h.serveImage(w, r)
	case path == "/doLogin":
		if host := h.form(r, "host"); host != "" && h.Proxy != nil {
			h.Proxy.SetHost(host)
		}
		h.doLogin(w, r)
	case path == "/logout" && method == http.MethodGet:
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
