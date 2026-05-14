package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"connectrpc.com/connect"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"

	platformv1 "github.com/anonde-io/anonde/gen/anonde/platform/v1"
	"github.com/anonde-io/anonde/gen/anonde/platform/v1/platformv1connect"
	"github.com/anonde-io/anonde/internal/core"
)

// DefaultMaxRequestBytes caps a single Connect request body. Configurable
// via NewHTTPServer/SetMaxRequestBytes; cmd/anonde/main reads
// MAX_CONTENT_BYTES. Connect enforces this via connect.WithReadMaxBytes,
// which returns ResourceExhausted (HTTP 429 over JSON) for oversized
// payloads. The REST gateway path does not enforce this today — see
// TODO.md.
const DefaultMaxRequestBytes int64 = 10 << 20 // 10 MiB

// HTTPServer fans the same Service out across three transports on one
// listener:
//
//   - REST/JSON via grpc-gateway:        /v1/...
//     (the public-facing surface, path-based URLs with verb suffixes)
//   - Connect/JSON + Connect/Protobuf:   /anonde.platform.v1.PlatformService/<Method>
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
	maxRequestBytes int64
}

func NewHTTPServer(svc *core.Service) *HTTPServer {
	return &HTTPServer{
		svc:             svc,
		connectServer:   NewConnectServer(svc),
		grpcServer:      NewGRPCServer(svc),
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

// Routes returns the wired http.Handler suitable for http.Server.
//
// Mount on an http.Server whose Protocols field has both HTTP/1.1 and
// UnencryptedHTTP2 enabled (see NewServerProtocols below) so a single
// port serves REST, Connect, and native gRPC without TLS.
func (s *HTTPServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)

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
	connectPath, connectHandler := platformv1connect.NewPlatformServiceHandler(s.connectServer, connectOpts...)
	mux.Handle(connectPath, connectHandler)

	// REST gateway: dispatches /v1/... requests in-process to the gRPC
	// server implementation (no separate gRPC port, no networking).
	// JSON shape mirrors the Connect codec above — snake_case on
	// output, tolerant of both shapes + unknown fields on input —
	// so callers see one consistent JSON contract across surfaces.
	gw := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
	)
	if err := platformv1.RegisterPlatformServiceHandlerServer(context.Background(), gw, s.grpcServer); err != nil {
		// Programmer error: only fires if codegen + registration drift.
		// Surfacing as panic keeps the wiring contract honest.
		panic("register grpc-gateway handler: " + err.Error())
	}
	mux.Handle("/v1/", gw)

	return loggingMiddleware(recoverMiddleware(corsMiddleware(mux)))
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
		// exposing the service publicly — see TODO.md.
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
