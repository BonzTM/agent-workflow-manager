package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	v1 "github.com/bonztm/agent-workflow-manager/internal/contracts/v1"
	"github.com/bonztm/agent-workflow-manager/internal/core"
)

// New returns an http.Handler that serves the AWM web UI and API.
// The projectID is used as the default for API calls that require one.
func New(svc core.Service, projectID string, static http.FileSystem) http.Handler {
	h := &handler{svc: svc, projectID: projectID}

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/plans", h.listPlans)
	mux.HandleFunc("GET /api/plans/{key}", h.getPlan)
	mux.HandleFunc("GET /api/status", h.getStatus)

	// Health check for k8s liveness/readiness probes.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	// Static assets (HTML, JS, CSS) — no-cache during development.
	if static != nil {
		fileServer := http.FileServer(static)
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			fileServer.ServeHTTP(w, r)
		}))
	}

	return mux
}

type handler struct {
	svc       core.Service
	projectID string
}

func (h *handler) listPlans(w http.ResponseWriter, r *http.Request) {
	scope := v1.HistoryScopeCurrent
	if q := r.URL.Query().Get("scope"); q != "" {
		switch v1.HistoryScope(q) {
		case v1.HistoryScopeCurrent, v1.HistoryScopeCompleted, v1.HistoryScopeDeferred, v1.HistoryScopeAll:
			scope = v1.HistoryScope(q)
		}
	}
	result, apiErr := h.svc.HistorySearch(r.Context(), v1.HistorySearchPayload{
		ProjectID: h.projectID,
		Entity:    v1.HistoryEntityWork,
		Scope:     scope,
	})
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) getPlan(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, `{"error":"missing plan key"}`, http.StatusBadRequest)
		return
	}

	// Plan keys use colons which are URL-unfriendly, so callers may use
	// the bare receipt ID. Normalize to the full plan key format.
	if !strings.HasPrefix(key, "plan:") {
		key = "plan:" + key
	}

	result, apiErr := h.svc.Export(r.Context(), v1.ExportPayload{
		ProjectID: h.projectID,
		Format:    v1.ExportFormatJSON,
		Fetch: &v1.ExportFetchSelector{
			Keys: []string{key},
		},
	})
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) getStatus(w http.ResponseWriter, r *http.Request) {
	result, apiErr := h.svc.Status(context.Background(), v1.StatusPayload{
		ProjectID: h.projectID,
	})
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeAPIError(w http.ResponseWriter, apiErr *core.APIError) {
	status := http.StatusInternalServerError
	if apiErr.Code == v1.ErrCodeNotFound {
		status = http.StatusNotFound
	} else if apiErr.Code == v1.ErrCodeValidationError || apiErr.Code == v1.ErrCodeInvalidPayload {
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   apiErr.Code,
		"message": apiErr.Message,
	})
}
