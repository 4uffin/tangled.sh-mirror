package knotserver

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/atproto/syntax"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-chi/chi/v5"
	"github.com/microcosm-cc/bluemonday"
)

func sanitize(content []byte) []byte {
	return bluemonday.UGCPolicy().SanitizeBytes([]byte(content))
}

func didPath(r *http.Request) string {
	did := chi.URLParam(r, "did")
	name := chi.URLParam(r, "name")
	path, _ := securejoin.SecureJoin(did, name)
	filepath.Clean(path)
	return path
}

func getDescription(path string) (desc string) {
	db, err := os.ReadFile(filepath.Join(path, "description"))
	if err == nil {
		desc = string(db)
	} else {
		desc = ""
	}
	return
}
func setContentDisposition(w http.ResponseWriter, name string) {
	h := "inline; filename=\"" + name + "\""
	w.Header().Add("Content-Disposition", h)
}

func setGZipMIME(w http.ResponseWriter) {
	setMIME(w, "application/gzip")
}

func setMIME(w http.ResponseWriter, mime string) {
	w.Header().Add("Content-Type", mime)
}

var TIDClock = syntax.NewTIDClock(0)

func TID() string {
	return TIDClock.Next().String()
}
