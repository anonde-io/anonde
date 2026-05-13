package platform

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	platformv1 "github.com/anonde-io/anonde/gen/anonde/platform/v1"
	"github.com/anonde-io/anonde/gen/anonde/platform/v1/platformv1connect"
)

// ConnectServer adapts Service to the generated Connect handler
// interface. The business logic (proto↔internal conversion plus the
// Service call) lives in proto_logic.go and is shared with the gRPC
// server impl; this file is just the connect.Request / connect.Response
// wrapper boilerplate plus the connect-specific error mapping.
type ConnectServer struct {
	svc *Service
}

var _ platformv1connect.PlatformServiceHandler = (*ConnectServer)(nil)

func NewConnectServer(svc *Service) *ConnectServer {
	return &ConnectServer{svc: svc}
}

func (h *ConnectServer) CreateAnonymization(ctx context.Context, req *connect.Request[platformv1.CreateAnonymizationRequest]) (*connect.Response[platformv1.CreateAnonymizationResponse], error) {
	resp, err := executeCreate(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) DetokenizeTokens(ctx context.Context, req *connect.Request[platformv1.DetokenizeTokensRequest]) (*connect.Response[platformv1.DetokenizeTokensResponse], error) {
	resp, err := executeDetokenize(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) RevealContent(ctx context.Context, req *connect.Request[platformv1.RevealContentRequest]) (*connect.Response[platformv1.RevealContentResponse], error) {
	resp, err := executeReveal(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) SynthesizeContent(ctx context.Context, req *connect.Request[platformv1.SynthesizeContentRequest]) (*connect.Response[platformv1.SynthesizeContentResponse], error) {
	resp, err := executeSynthesize(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) DeleteAnonymization(ctx context.Context, req *connect.Request[platformv1.DeleteAnonymizationRequest]) (*connect.Response[platformv1.DeleteAnonymizationResponse], error) {
	resp, err := executeDelete(ctx, h.svc, req.Msg)
	if err != nil {
		return nil, connectErrFor(err, connect.CodeInvalidArgument)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) GetVersion(ctx context.Context, _ *connect.Request[platformv1.GetVersionRequest]) (*connect.Response[platformv1.GetVersionResponse], error) {
	resp, err := executeGetVersion(ctx, h.svc)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *ConnectServer) HealthCheck(_ context.Context, _ *connect.Request[platformv1.HealthCheckRequest]) (*connect.Response[platformv1.HealthCheckResponse], error) {
	return connect.NewResponse(executeHealthCheck()), nil
}

// connectErrFor maps a Service error to a connect.Error. Policy denials
// map to PermissionDenied (HTTP 403 in Connect/JSON). Everything else
// uses the supplied fallback code (typically InvalidArgument for
// validation failures in the caller).
func connectErrFor(err error, fallback connect.Code) *connect.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrPolicyDenied) {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	return connect.NewError(fallback, err)
}
