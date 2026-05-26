package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	anondev1 "github.com/anonde-io/anonde/gen/anonde/v1"
)

// PDF endpoints live inside the proto-defined Service. This file holds
// the grpc-gateway extras the binary surface needs:
//
//   - pdfMarshaler: a runtime.Marshaler keyed on Content-Type
//     "application/pdf" so the REST gateway reads/writes raw PDF bytes
//     instead of base64-in-JSON. The wildcard JSONPb marshaler still
//     handles every other content type — this only kicks in when a
//     caller explicitly asks for / sends application/pdf.
//
//   - tenantMetadataAnnotator: pulls X-Anonde-Tenant from the inbound
//     HTTP request into gRPC metadata so executeAnonymizePDF /
//     executeRevealPDF can read it as a fallback when the proto
//     tenant_id field is empty (Stripe-style header binding without
//     requiring it in the URL).
//
//   - pdfForwardResponse: writes the X-Anonde-Id / X-Anonde-Entities /
//     X-Anonde-Entity-Count headers from the proto response message so
//     REST clients can log counts without parsing the PDF body.

// mimeApplicationPDF is the request / response Content-Type the PDF
// endpoints use over REST. Kept here (not in pdf.go) because pdf.go is
// going away — this file owns the gateway-side PDF wiring.
const mimeApplicationPDF = "application/pdf"

// metadataKeyTenant is the gRPC metadata key the REST gateway carries
// the X-Anonde-Tenant header under. Lowercase per gRPC metadata
// convention; downstream readers use lowercase too.
const metadataKeyTenant = "anonde-tenant"

// headerTenant is the inbound header name. Exposed as a const so tests
// and the metadata annotator stay in sync with the documented API.
const headerTenant = "X-Anonde-Tenant"

// tenantMetadataAnnotator returns a runtime.WithMetadata option that
// copies the X-Anonde-Tenant header into gRPC metadata so the
// transport-agnostic execute* functions can read it.
func tenantMetadataAnnotator(_ context.Context, r *http.Request) metadata.MD {
	md := metadata.MD{}
	if v := strings.TrimSpace(r.Header.Get(headerTenant)); v != "" {
		md.Set(metadataKeyTenant, v)
	}
	return md
}

// tenantFromIncomingMD reads the tenant id the REST gateway forwarded
// via the metadata annotator. Returns "" when called from a non-REST
// surface (gRPC / Connect) or when no header was set; callers are
// expected to fall back to msg.GetTenantId() first.
func tenantFromIncomingMD(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vs := md.Get(metadataKeyTenant)
	if len(vs) == 0 {
		return ""
	}
	return strings.TrimSpace(vs[0])
}

// pdfMarshaler implements runtime.Marshaler for raw application/pdf
// payloads. It's intentionally narrow on the success path — it only
// knows about the PDF request/response messages and the *[]byte form
// grpc-gateway uses when `body: "pdf_content"` binds a single bytes
// field.
//
// The error path is different: grpc-gateway calls Marshal on a
// *status.Status to encode RPC errors, using whichever marshaler the
// request's Content-Type selected. Returning an error from Marshal
// here would yield a 500 with a useless "failed to marshal error
// message" body, hiding the real status code. We delegate everything
// we don't recognise to a JSONPb marshaler so error responses come
// back as JSON with the correct HTTP status, while successful PDF
// responses stay raw bytes.
type pdfMarshaler struct {
	// errFallback marshals anything that isn't a PDF success
	// response (typically a *status.Status). Configured to match the
	// JSON shape the wildcard marshaler uses, so the JSON error body
	// looks identical regardless of how the request was routed.
	errFallback runtime.JSONPb
}

func newPDFMarshaler() pdfMarshaler {
	return pdfMarshaler{
		errFallback: runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		},
	}
}

// isPDFResponse reports whether v is one of the success-path message
// types this marshaler writes as raw bytes.
func isPDFResponse(v interface{}) bool {
	switch v.(type) {
	case *anondev1.AnonymizePDFResponse, *anondev1.RevealPDFResponse, []byte, *[]byte:
		return true
	default:
		return false
	}
}

// ContentType returns application/pdf for PDF success responses and
// application/json for everything else (errors). The gateway calls
// this with the message it's about to write, so the response
// Content-Type tracks the actual payload shape.
func (m pdfMarshaler) ContentType(v interface{}) string {
	if isPDFResponse(v) {
		return mimeApplicationPDF
	}
	return m.errFallback.ContentType(v)
}

// Marshal writes the raw PDF bytes out of a response proto. The
// response messages embed the bytes in a dedicated field
// (RedactedPdf / OriginalPdf); we type-switch on the message to pull
// the right one. Anything else (status.Status on the error path)
// flows through the JSON fallback.
func (m pdfMarshaler) Marshal(v interface{}) ([]byte, error) {
	switch t := v.(type) {
	case *anondev1.AnonymizePDFResponse:
		return t.GetRedactedPdf(), nil
	case *anondev1.RevealPDFResponse:
		return t.GetOriginalPdf(), nil
	case []byte:
		return t, nil
	case *[]byte:
		if t == nil {
			return nil, nil
		}
		return *t, nil
	default:
		return m.errFallback.Marshal(v)
	}
}

// Unmarshal copies the raw request body into the proto bytes field.
// grpc-gateway invokes Unmarshal with either the whole request message
// (when body: "*") or with a pointer to the bound field (when body:
// "field_name"). We registered the PDF rpc with body: "pdf_content"
// (a single bytes field), so the gateway passes *[]byte here.
func (pdfMarshaler) Unmarshal(data []byte, v interface{}) error {
	switch t := v.(type) {
	case *[]byte:
		*t = append((*t)[:0], data...)
		return nil
	case *anondev1.AnonymizePDFRequest:
		t.PdfContent = append(t.PdfContent[:0], data...)
		return nil
	default:
		return fmt.Errorf("pdfMarshaler: cannot unmarshal application/pdf into %T", v)
	}
}

// NewDecoder / NewEncoder are required by the Marshaler interface but
// only matter for streaming RPCs. Our PDF surface is unary; provide
// trivial implementations that buffer the whole payload.
func (m pdfMarshaler) NewDecoder(r io.Reader) runtime.Decoder {
	return runtime.DecoderFunc(func(v interface{}) error {
		raw, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		return m.Unmarshal(raw, v)
	})
}

func (m pdfMarshaler) NewEncoder(w io.Writer) runtime.Encoder {
	return runtime.EncoderFunc(func(v interface{}) error {
		raw, err := m.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(raw)
		return err
	})
}

// pdfForwardResponse writes the X-Anonde-* headers from the proto
// response onto the REST response. Runs after the gateway has decided
// to write a successful body but before the body is sent, so headers
// are still mutable.
//
// Returns nil unconditionally — failure here would mean a programmer
// error (response shape changed without updating this fn) and we'd
// rather log and continue than block the body. The Set/Add calls are
// already best-effort once the response has been touched.
func pdfForwardResponse(_ context.Context, w http.ResponseWriter, msg proto.Message) error {
	switch m := msg.(type) {
	case *anondev1.AnonymizePDFResponse:
		w.Header().Set("X-Anonde-Id", m.GetId())
		w.Header().Set("X-Anonde-Tenant", m.GetTenantId())
		w.Header().Set("X-Anonde-Entities", strconv.Itoa(int(m.GetEntitiesCount())))
		w.Header().Set("X-Anonde-Entity-Types", strconv.Itoa(int(m.GetEntityTypesCount())))
		for entityType, n := range m.GetEntitiesByType() {
			w.Header().Add("X-Anonde-Entity-Count", fmt.Sprintf("%s=%d", entityType, n))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(m.GetRedactedPdf())))
	case *anondev1.RevealPDFResponse:
		w.Header().Set("X-Anonde-Id", m.GetId())
		w.Header().Set("Content-Length", strconv.Itoa(len(m.GetOriginalPdf())))
	}
	return nil
}
