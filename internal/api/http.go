package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"

	anondev1 "github.com/anonde-io/anonde/gen/anonde/v1"
	"github.com/anonde-io/anonde/gen/anonde/v1/anondev1connect"
	"github.com/anonde-io/anonde/internal/core"
)

// DefaultMaxRequestBytes caps a single Connect request body. Configurable
// via NewHTTPServer/SetMaxRequestBytes; cmd/anonde/main reads
// MAX_CONTENT_BYTES. Connect enforces this via connect.WithReadMaxBytes,
// which returns ResourceExhausted (HTTP 429 over JSON) for oversized
// payloads. The REST gateway path does not enforce this today — a
// gateway-level body-cap middleware is the gap.
const DefaultMaxRequestBytes int64 = 10 << 20 // 10 MiB

// HTTPServer fans the same Service out across three transports on one
// listener:
//
//   - REST/JSON via grpc-gateway:        /v1/...
//     (the public-facing surface, path-based URLs with verb suffixes)
//   - Connect/JSON + Connect/Protobuf:   /anonde.v1.Service/<Method>
//     (Connect SDK clients, gRPC-Web)
//   - native gRPC (over HTTP/2):         same Connect URL, content-negotiated
//     (Connect's handler also speaks the gRPC wire protocol)
//
// Plus a plain-HTTP /healthz for the container scheduler's healthcheck. The gateway and the
// Connect handler share the underlying Service, so behaviour is
// identical across surfaces; only error mapping differs (gRPC codes
// vs connect.Code) and that's handled in proto_logic.go's siblings.
type HTTPServer struct {
	svc             *core.Service
	connectServer   *ConnectServer
	grpcServer      *GRPCServer
	proxy           *openAIProxy
	maxRequestBytes int64
}

func NewHTTPServer(svc *core.Service) *HTTPServer {
	return &HTTPServer{
		svc:           svc,
		connectServer: NewConnectServer(svc),
		grpcServer:    NewGRPCServer(svc),
		// The OpenAI-compatible proxy is always mounted; a zero
		// OpenAIProxyConfig resolves to OpenAI as the upstream.
		// cmd/anonde overrides this from env via SetOpenAIProxy.
		proxy:           newOpenAIProxy(svc, OpenAIProxyConfig{}),
		maxRequestBytes: DefaultMaxRequestBytes,
	}
}

// SetMaxRequestBytes overrides the per-request body cap. Use 0 to disable.
func (s *HTTPServer) SetMaxRequestBytes(n int64) {
	if n < 0 {
		n = 0
	}
	s.maxRequestBytes = n
}

// SetOpenAIProxy configures the OpenAI upstream the proxy endpoint
// (POST /v1/chat/completions) forwards to. Call before Routes();
// cmd/anonde wires this from ANONDE_OPENAI_* env vars.
func (s *HTTPServer) SetOpenAIProxy(cfg OpenAIProxyConfig) {
	s.proxy = newOpenAIProxy(s.svc, cfg)
}

// Routes returns the wired http.Handler suitable for http.Server.
//
// Mount on an http.Server whose Protocols field has both HTTP/1.1 and
// UnencryptedHTTP2 enabled (see NewServerProtocols below) so a single
// port serves REST, Connect, and native gRPC without TLS.
func (s *HTTPServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)

	// OpenAI-compatible proxy. POST /v1/chat/completions is a strictly
	// more specific pattern than the "/v1/" subtree handed to the REST
	// gateway below, so ServeMux routes it without conflict. Wrapped in
	// the request-body cap because, unlike the Connect handler, this
	// path reads r.Body directly.
	proxyHandler := http.HandlerFunc(s.proxy.chatCompletions)
	mux.Handle(chatCompletionsPath, s.limitBody(proxyHandler))

	connectOpts := []connect.HandlerOption{
		// Replace Connect's default JSON codec with one that uses
		// snake_case proto names on the wire (UseProtoNames=true).
		// Input still accepts both snake_case and camelCase per the
		// proto3 JSON spec.
		connect.WithCodec(newSnakeCaseJSONCodec()),
	}
	if s.maxRequestBytes > 0 {
		connectOpts = append(connectOpts, connect.WithReadMaxBytes(int(s.maxRequestBytes)))
	}
	connectPath, connectHandler := anondev1connect.NewServiceHandler(s.connectServer, connectOpts...)
	mux.Handle(connectPath, connectHandler)

	// REST gateway: dispatches /v1/... requests in-process to the gRPC
	// server implementation (no separate gRPC port, no networking).
	// JSON shape mirrors the Connect codec above; snake_case on
	// output, tolerant of both shapes + unknown fields on input,
	// so callers see one consistent JSON contract across surfaces.
	//
	// Two extra marshaler hooks layered in for the PDF surface:
	//   - application/pdf is handled by pdfMarshaler (raw bytes in/out,
	//     no base64-in-JSON). Other content types still flow through
	//     the JSONPb wildcard marshaler.
	//   - tenantMetadataAnnotator copies X-Anonde-Tenant from the HTTP
	//     request into gRPC metadata so executeAnonymizePDF /
	//     executeRevealPDF can read it as a fallback.
	//   - pdfForwardResponse writes the X-Anonde-Id / -Entities /
	//     -Entity-Count headers from the response proto.
	gw := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
		runtime.WithMarshalerOption(mimeApplicationPDF, newPDFMarshaler()),
		runtime.WithMetadata(tenantMetadataAnnotator),
		runtime.WithForwardResponseOption(pdfForwardResponse),
	)
	if err := anondev1.RegisterServiceHandlerServer(context.Background(), gw, s.grpcServer); err != nil {
		// Programmer error: only fires if codegen + registration drift.
		// Surfacing as panic keeps the wiring contract honest.
		panic("register grpc-gateway handler: " + err.Error())
	}

	// Direct handler for GET /v1/anonymizations/{id}/reveal-pdf.
	// Mounted ahead of the gateway subtree so the ServeMux's
	// most-specific-pattern-wins rule picks this over the catch-all
	// gateway. Why bypass the gateway: grpc-gateway selects the
	// response marshaler from the Accept header; with the default
	// Accept: */* it falls back to JSON, then pdfForwardResponse
	// declares a Content-Length sized for the raw PDF (from the
	// response proto field), and the JSON body; which base64-encodes
	// the PDF; exceeds it. Result: "wrote more than declared
	// Content-Length" and a truncated body. A dedicated handler that
	// writes raw bytes makes the GET behave like an asset fetch
	// regardless of Accept, matching the pre-PR-#11 hand-rolled shape.
	mux.HandleFunc("GET /v1/anonymizations/{id}/reveal-pdf", s.revealPDF)

	// Body cap on the REST gateway subtree. Connect already enforces
	// MAX_CONTENT_BYTES via connect.WithReadMaxBytes above; without this
	// wrap the REST path used to accept arbitrary-size bodies — a real
	// DoS / OOM vector flagged by the e2e + stress suites.
	mux.Handle("/v1/", s.limitBody(gw))

	return auditMiddleware(recoverMiddleware(corsMiddleware(mux)))
}

// revealPDF bypasses grpc-gateway for the reveal-pdf GET so that the
// response is always raw application/pdf bytes regardless of the
// caller's Accept header. Mirrors the gateway's tenant-binding
// behaviour: prefers the X-Anonde-Tenant header, then ?tenant=<id>,
// ?tenant_id=<id>, ?tenantId=<id> query params.
func (s *HTTPServer) revealPDF(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get(headerTenant))
	if tenantID == "" {
		q := r.URL.Query()
		for _, k := range []string{"tenant", "tenant_id", "tenantId"} {
			if v := strings.TrimSpace(q.Get(k)); v != "" {
				tenantID = v
				break
			}
		}
	}
	if tenantID == "" {
		http.Error(w, "tenant_id required (set X-Anonde-Tenant header or ?tenant=<id>)", http.StatusBadRequest)
		return
	}
	raw, err := s.svc.GetOriginalPDF(r.Context(), tenantID, id)
	if err != nil {
		// GetOriginalPDF returns ErrNotFound-ish strings for missing
		// records; map every error to 404 here so callers don't get a
		// 500 for an expired/deleted id. The error body keeps the
		// detail for debugging.
		code := http.StatusNotFound
		if errors.Is(err, core.ErrPDFRedactorUnconfigured) {
			code = http.StatusNotImplemented
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.Header().Set("Content-Type", mimeApplicationPDF)
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.Header().Set("X-Anonde-Id", id)
	w.Header().Set("X-Anonde-Tenant", tenantID)
	_, _ = w.Write(raw)
}

// NewServerProtocols returns the http.Protocols value needed so a single
// TCP listener can serve HTTP/1.1 (browsers, curl, REST gateway) and
// unencrypted HTTP/2 (native gRPC clients) at once. Without
// UnencryptedHTTP2, gRPC over cleartext would not work without TLS.
func NewServerProtocols() *http.Protocols {
	p := &http.Protocols{}
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

// limitBody caps the request body using http.MaxBytesReader, mirroring
// the connect.WithReadMaxBytes guard the Connect handler gets. A no-op
// when maxRequestBytes is 0 (cap disabled).
func (s *HTTPServer) limitBody(next http.Handler) http.Handler {
	if s.maxRequestBytes <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Audit-log heartbeat cadence. Hardcoded by design — these are
// operator-facing thresholds for "is this request stuck?" visibility, not
// per-deploy tuning knobs. The values are `var` (not const) only so tests
// can override them for fast assertions; do not mutate from production code.
var (
	auditHeartbeatThreshold = 2 * time.Second
	auditHeartbeatInterval  = 5 * time.Second
)

// requestIDHeader is the response header surfaced to callers so they can
// correlate their client-side errors with a server-side audit trail.
const requestIDHeader = "X-Anonde-Request-Id"

type ctxKey int

const requestIDCtxKey ctxKey = 1

// RequestIDFromContext returns the request id assigned by the audit
// middleware, or "" if the request did not flow through it (e.g. tests
// calling handlers directly).
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDCtxKey).(string)
	return v
}

// auditLogger is the package-level audit sink. JSON to stderr so
// self-hosters can pipe into Loki / CloudWatch / Vector without
// re-parsing. Mixed with the codebase's existing log.Printf lines on the
// same stream — operators who want one format can migrate the rest.
var auditLogger = slog.New(slog.NewJSONHandler(os.Stderr, nil))

// SetAuditLogger overrides the audit middleware logger. Call before
// Routes(); intended for tests and embedders that want to route audit
// events to their own slog handler.
func SetAuditLogger(l *slog.Logger) {
	if l != nil {
		auditLogger = l
	}
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand can't fail in practice; if it does, fall back to a
		// timestamp-based id so requests still get a (less unique) tag.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

type statusRecorder struct {
	http.ResponseWriter
	status       int
	bytesOut     int64
	wroteHeader  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.bytesOut += int64(n)
	return n, err
}

// Flush passes through so chunked / streaming responses (e.g. the OpenAI
// proxy in stream mode) keep working under the audit wrap.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// auditMiddleware logs every request through a structured audit trail:
//   - request_start on entry (method, path, remote, content_length).
//   - request_inflight heartbeat after auditHeartbeatThreshold and every
//     auditHeartbeatInterval after that — surfaces "is the PDF still
//     working?" without instrumenting individual handlers.
//   - request_end with status, duration, and bytes written on completion.
//
// /healthz is skipped because any container scheduler hits it on
// a sub-minute cadence and would otherwise drown the audit log.
func auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		id := newRequestID()
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDCtxKey, id)
		r = r.WithContext(ctx)

		start := time.Now()
		auditLogger.LogAttrs(ctx, slog.LevelInfo, "request_start",
			slog.String("request_id", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote", r.RemoteAddr),
			slog.Int64("bytes_in", r.ContentLength),
		)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// Heartbeat goroutine. Does not touch rec.status / rec.bytesOut to
		// stay race-free with the handler goroutine — heartbeats only
		// report elapsed time + immutable request metadata. We wait for
		// the goroutine to exit (hbWg.Wait) before returning so no
		// goroutine outlives the request; otherwise a delayed heartbeat
		// could fire AFTER request_end, and any read of the heartbeat
		// tuning vars would race against test cleanup.
		done := make(chan struct{})
		var hbWg sync.WaitGroup
		hbWg.Go(func() {
			timer := time.NewTimer(auditHeartbeatThreshold)
			defer timer.Stop()
			for {
				select {
				case <-done:
					return
				case <-timer.C:
					auditLogger.LogAttrs(ctx, slog.LevelInfo, "request_inflight",
						slog.String("request_id", id),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.Int64("elapsed_ms", time.Since(start).Milliseconds()),
					)
					timer.Reset(auditHeartbeatInterval)
				}
			}
		})

		next.ServeHTTP(rec, r)
		close(done)
		hbWg.Wait()

		auditLogger.LogAttrs(ctx, slog.LevelInfo, "request_end",
			slog.String("request_id", id),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.Int64("bytes_out", rec.bytesOut),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local dev default: allow browser clients from other localhost
		// ports. Becomes unsafe once auth lands (browser pages with
		// auth cookies could call Reveal); a `CORS_ALLOW_ORIGINS`
		// env var defaulting to `*` only when empty is the planned fix.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		// X-Anonde-Tenant is the Stripe-style tenant binding for PDF
		// (and any future per-tenant) endpoints; allow it on the
		// inbound side so browser preflights succeed.
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,Connect-Protocol-Version,Connect-Timeout-Ms,X-Anonde-Tenant")
		// Expose the PDF response headers (id, tenant, entity counts)
		// so browser-side clients like the lens demo can read them
		// off fetch responses without parsing the PDF body.
		w.Header().Set("Access-Control-Expose-Headers", "X-Anonde-Id,X-Anonde-Tenant,X-Anonde-Entities,X-Anonde-Entity-Types,X-Anonde-Entity-Count")
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
				auditLogger.LogAttrs(r.Context(), slog.LevelError, "request_panic",
					slog.String("request_id", RequestIDFromContext(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal server error"})
			}
		}()

		next.ServeHTTP(w, r)
	})
}
