package platform

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

type HTTPServer struct {
	svc *Service
}

func NewHTTPServer(svc *Service) *HTTPServer {
	return &HTTPServer{svc: svc}
}

func (s *HTTPServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/v1/ingest", s.ingest)
	mux.HandleFunc("/v1/detokenize", s.detokenize)
	mux.HandleFunc("/v1/reveal", s.reveal)
	return loggingMiddleware(recoverMiddleware(corsMiddleware(mux)))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("http request method=%s path=%s status=%d duration_ms=%d remote=%s", r.Method, r.URL.Path, rec.status, time.Since(start).Milliseconds(), r.RemoteAddr)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local dev default: allow browser clients from other localhost ports.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	resp, err := s.svc.Ingest(r.Context(), req)
	if err != nil {
		log.Printf("usage ingest tenant=%q doc=%q bytes=%d error=%q", req.TenantID, req.DocID, len(req.Content), err)
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	log.Printf("usage ingest tenant=%q doc=%q bytes=%d entities=%d tokens=%d", req.TenantID, req.DocID, len(req.Content), resp.DetectedEntitySize, len(resp.Tokens))
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) detokenize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req DetokenizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	resp, err := s.svc.Detokenize(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrPolicyDenied) {
			status = http.StatusForbidden
		}
		log.Printf("usage detokenize tenant=%q doc=%q actor=%q purpose=%q tokens=%d status=%d error=%q", req.TenantID, req.DocID, req.Actor, req.Purpose, len(req.Tokens), status, err)
		writeErr(w, status, err)
		return
	}
	log.Printf("usage detokenize tenant=%q doc=%q actor=%q purpose=%q requested_tokens=%d resolved_tokens=%d", req.TenantID, req.DocID, req.Actor, req.Purpose, len(req.Tokens), len(resp.Resolved))
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) reveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req RevealRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	resp, err := s.svc.Reveal(r.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrPolicyDenied) {
			status = http.StatusForbidden
		}
		log.Printf("usage reveal tenant=%q doc=%q actor=%q purpose=%q content_bytes=%d status=%d error=%q", req.TenantID, req.DocID, req.Actor, req.Purpose, len(req.Content), status, err)
		writeErr(w, status, err)
		return
	}
	log.Printf("usage reveal tenant=%q doc=%q actor=%q purpose=%q content_bytes=%d resolved_tokens=%d", req.TenantID, req.DocID, req.Actor, req.Purpose, len(req.Content), len(resp.Resolved))
	writeJSON(w, http.StatusOK, resp)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
