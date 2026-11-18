package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"reviewboard-mcp/internal/staging"
)

// uploadHandler stages a UTF-8 body under a single-use id that the write tools
// accept as *_upload_id.
func uploadHandler(store *staging.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if store == nil {
			http.Error(w, "uploads are disabled", http.StatusServiceUnavailable)
			return
		}
		filename := r.URL.Query().Get("filename")
		if filename == "" {
			http.Error(w, "filename is required", http.StatusBadRequest)
			return
		}
		info, err := store.Stage(r.Body, filename)
		switch {
		case errors.Is(err, staging.ErrTooLarge):
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		case errors.Is(err, staging.ErrEmpty), errors.Is(err, staging.ErrNotText):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(info)
	})
}
