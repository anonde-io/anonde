package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
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
// payloads. The REST gateway path does not enforce this today; see
// TODO.md.
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
// Plus a plain-HTTP /healthz for Fly's healthcheck. The gateway and the
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

	mux.Handle("/v1/", gw)

	return loggingMiddleware(recoverMiddleware(corsMiddleware(mux)))
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
		log.Printf("http request method=%s path=%s status=%d duration_ms=%d remote=%s",
			r.Method, r.URL.Path, rec.status, time.Since(start).Milliseconds(), r.RemoteAddr)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local dev default: allow browser clients from other localhost
		// ports. Tighten via a CORS_ALLOW_ORIGINS env var before
		// exposing the service publicly; see TODO.md.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,Connect-Protocol-Version,Connect-Timeout-Ms")
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
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal server error"})
			}
		}()

		next.ServeHTTP(w, r)
	})
}
