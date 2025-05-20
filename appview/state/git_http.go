package state

import (
	"fmt"
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/go-chi/chi/v5"
)

func (s *State) InfoRefs(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("resolvedId").(identity.Identity)
	knot := r.Context().Value("knot").(string)
	repo := chi.URLParam(r, "repo")

	scheme := "https"
	if s.config.Core.Dev {
		scheme = "http"
	}

	targetURL := fmt.Sprintf("%s://%s/%s/%s/info/refs?%s", scheme, knot, user.DID, repo, r.URL.RawQuery)
	s.proxyRequest(w, r, targetURL)

}

func (s *State) UploadPack(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("resolvedId").(identity.Identity)
	if !ok {
		http.Error(w, "failed to resolve user", http.StatusInternalServerError)
		return
	}
	knot := r.Context().Value("knot").(string)
	repo := chi.URLParam(r, "repo")

	scheme := "https"
	if s.config.Core.Dev {
		scheme = "http"
	}

	targetURL := fmt.Sprintf("%s://%s/%s/%s/git-upload-pack?%s", scheme, knot, user.DID, repo, r.URL.RawQuery)
	s.proxyRequest(w, r, targetURL)
}

func (s *State) ReceivePack(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("resolvedId").(identity.Identity)
	if !ok {
		http.Error(w, "failed to resolve user", http.StatusInternalServerError)
		return
	}
	knot := r.Context().Value("knot").(string)
	repo := chi.URLParam(r, "repo")

	scheme := "https"
	if s.config.Core.Dev {
		scheme = "http"
	}

	targetURL := fmt.Sprintf("%s://%s/%s/%s/git-receive-pack?%s", scheme, knot, user.DID, repo, r.URL.RawQuery)
	s.proxyRequest(w, r, targetURL)
}

func (s *State) proxyRequest(w http.ResponseWriter, r *http.Request, targetURL string) {
	client := &http.Client{}

	// Create new request
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy original headers
	proxyReq.Header = r.Header

	// Execute request
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	// Set response status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
