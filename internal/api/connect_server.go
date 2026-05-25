package api

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	anondev1 "github.com/anonde-io/anonde/gen/anonde/v1"
	"github.com/anonde-io/anonde/gen/anonde/v1/anondev1connect"
	"github.com/anonde-io/anonde/internal/core"
)

// ConnectServer adapts Service to the generated Connect handler
// interface. The business logic (proto↔internal conversion plus the
// Service call) lives in proto_logic.go and is shared with the gRPC
// server impl; this file is just the connect.Request / connect.Response
// wrapper boilerplate plus the connect-specific error mapping.
type ConnectServer struct {
	svc *core.Service
}

var _ anondev1connect.ServiceHandler = (*ConnectServer)(nil)

func NewConnectServer(svc *core.Service) *ConnectServer {
	return &ConnectServer{svc: svc}
}

func (h *ConnectServer) CreateAnonymization(ctx context.Context, req *connect.Request[anondev1.CreateAnonymizationRequest]) (*connect.Response[anondev1.CreateAnonymizationResponse], error) {
	resp, err := executeCreate(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) DetokenizeTokens(ctx context.Context, req *connect.Request[anondev1.DetokenizeTokensRequest]) (*connect.Response[anondev1.DetokenizeTokensResponse], error) {
	resp, err := executeDetokenize(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) RevealContent(ctx context.Context, req *connect.Request[anondev1.RevealContentRequest]) (*connect.Response[anondev1.RevealContentResponse], error) {
	resp, err := executeReveal(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) SynthesizeContent(ctx context.Context, req *connect.Request[anondev1.SynthesizeContentRequest]) (*connect.Response[anondev1.SynthesizeContentResponse], error) {
	resp, err := executeSynthesize(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) DeleteAnonymization(ctx context.Context, req *connect.Request[anondev1.DeleteAnonymizationRequest]) (*connect.Response[anondev1.DeleteAnonymizationResponse], error) {
	resp, err := executeDelete(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) GetVersion(ctx context.Context, _ *connect.Request[anondev1.GetVersionRequest]) (*connect.Response[anondev1.GetVersionResponse], error) {
	resp, err := executeGetVersion(ctx, h.svc)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) HealthCheck(_ context.Context, _ *connect.Request[anondev1.HealthCheckRequest]) (*connect.Response[anondev1.HealthCheckResponse], error) {
	return connect.NewResponse(executeHealthCheck()), nil
}

// connectErrFor maps a Service error to a connect.Error. Policy denials
// map to PermissionDenied (HTTP 403 in Connect/JSON); unconfigured PDF
// redactor maps to Unimplemented (HTTP 501) so callers see the same
// signal across REST and Connect. Everything else uses the supplied
// fallback code (typically InvalidArgument for validation failures in
// the caller).
func connectErrFor(err error, fallback connect.Code) *connect.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, core.ErrPolicyDenied) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	if errors.Is(err, core.ErrPDFRedactorUnconfigured) {
		return connect.NewError(connect.CodeUnimplemented, err)
	}
	return connect.NewError(fallback, err)
}

func (h *ConnectServer) AnonymizePDF(ctx context.Context, req *connect.Request[anondev1.AnonymizePDFRequest]) (*connect.Response[anondev1.AnonymizePDFResponse], error) {
	resp, err := executeAnonymizePDF(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) RevealPDF(ctx context.Context, req *connect.Request[anondev1.RevealPDFRequest]) (*connect.Response[anondev1.RevealPDFResponse], error) {
	resp, err := executeRevealPDF(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}
