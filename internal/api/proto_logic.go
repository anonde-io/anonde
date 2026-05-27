package api

import (
	"context"

	"github.com/anonde-io/anonde/analyzer"
	"github.com/anonde-io/anonde/internal/core"
	anondev1 "github.com/anonde-io/anonde/gen/anonde/v1"
)

// proto_logic.go centralises the proto↔internal conversion used by
// every transport (Connect, gRPC, REST-via-grpc-gateway). The handler
// types stay thin: they take protocol-specific request envelopes,
// pull the inner message out, call the executeX functions here, then
// wrap the returned native error in the protocol's error type.
//
// Service stays unaware of proto; it owns business logic against the
// internal IngestRequest / DetokenizeRequest / … types.

func executeCreate(ctx context.Context, svc *core.Service, msg *anondev1.CreateAnonymizationRequest) (*anondev1.CreateAnonymizationResponse, error) {
	req := core.IngestRequest{
		TenantID:      msg.GetTenantId(),
		ID:            msg.GetId(),
		Content:       msg.GetContent(),
		ContentFormat: msg.GetContentFormat(),
	}
	applyAnalyzerOptions(&req.Language, &req.Entities, &req.ScoreThreshold, &req.DisableNER, msg.GetOptions())

	resp, err := svc.Ingest(ctx, req)
	if err != nil {
		return nil, err
	}
	return &anondev1.CreateAnonymizationResponse{
		TenantId:           resp.TenantID,
		Id:                 resp.ID,
		AnonymizedContent:  resp.AnonymizedContent,
		DetectedEntitySize: int32(resp.DetectedEntitySize),
		Findings:           findingsToProto(resp.Findings),
		Tokens:             tokensToProto(resp.Tokens),
	}, nil
}

func executeDetokenize(ctx context.Context, svc *core.Service, msg *anondev1.DetokenizeTokensRequest) (*anondev1.DetokenizeTokensResponse, error) {
	resp, err := svc.Detokenize(ctx, core.DetokenizeRequest{
		TenantID: msg.GetTenantId(),
		ID:       msg.GetId(),
		Actor:    msg.GetActor(),
		Purpose:  msg.GetPurpose(),
		Tokens:   msg.GetTokens(),
	})
	if err != nil {
		return nil, err
	}
	return &anondev1.DetokenizeTokensResponse{
		TenantId: resp.TenantID,
		Id:       resp.ID,
		Resolved: resp.Resolved,
	}, nil
}

func executeReveal(ctx context.Context, svc *core.Service, msg *anondev1.RevealContentRequest) (*anondev1.RevealContentResponse, error) {
	resp, err := svc.Reveal(ctx, core.RevealRequest{
		TenantID:      msg.GetTenantId(),
		ID:            msg.GetId(),
		Actor:         msg.GetActor(),
		Purpose:       msg.GetPurpose(),
		Content:       msg.GetContent(),
		ContentFormat: msg.GetContentFormat(),
	})
	if err != nil {
		return nil, err
	}
	return &anondev1.RevealContentResponse{
		TenantId:            resp.TenantID,
		Id:                  resp.ID,
		DeanonymizedContent: resp.DeanonymizedContent,
		Resolved:            resp.Resolved,
	}, nil
}

func executeSynthesize(ctx context.Context, svc *core.Service, msg *anondev1.SynthesizeContentRequest) (*anondev1.SynthesizeContentResponse, error) {
	req := core.SynthesizeRequest{
		Content:       msg.GetContent(),
		ContentFormat: msg.GetContentFormat(),
		Consistent:    msg.GetConsistent(),
		DocScoped:     msg.GetDocScoped(),
	}
	applyAnalyzerOptions(&req.Language, &req.Entities, &req.ScoreThreshold, &req.DisableNER, msg.GetOptions())

	resp, err := svc.Synthesize(ctx, req)
	if err != nil {
		return nil, err
	}
	return &anondev1.SynthesizeContentResponse{
		Content:  resp.Content,
		Findings: findingsToProto(resp.Findings),
	}, nil
}

func executeDelete(ctx context.Context, svc *core.Service, msg *anondev1.DeleteAnonymizationRequest) (*anondev1.DeleteAnonymizationResponse, error) {
	result, err := svc.DeleteAnonymization(ctx, msg.GetTenantId(), msg.GetId())
	if err != nil {
		return nil, err
	}
	return &anondev1.DeleteAnonymizationResponse{
		Deleted:       result.Deleted,
		TokensDeleted: int32(result.TokensDeleted),
	}, nil
}

func executeGetVersion(ctx context.Context, svc *core.Service) (*anondev1.GetVersionResponse, error) {
	info, err := svc.GetVersion(ctx)
	if err != nil {
		return nil, err
	}
	return &anondev1.GetVersionResponse{
		AnalyzerBackend: info.AnalyzerBackend,
		Model:           info.Model,
		BuildSha:        info.BuildSHA,
		GoVersion:       info.GoVersion,
		ApiVersion:      info.APIVersion,
	}, nil
}

func executeHealthCheck() *anondev1.HealthCheckResponse {
	return &anondev1.HealthCheckResponse{
		Status: anondev1.HealthCheckResponse_SERVING_STATUS_SERVING,
	}
}

func executeAnonymizePDF(ctx context.Context, svc *core.Service, msg *anondev1.AnonymizePDFRequest) (*anondev1.AnonymizePDFResponse, error) {
	tenantID := msg.GetTenantId()
	if tenantID == "" {
		// Fall back to the X-Anonde-Tenant header carried in incoming
		// gRPC metadata by the REST gateway's metadata annotator. gRPC /
		// Connect callers populate the proto field directly so this
		// fallback is REST-only.
		tenantID = tenantFromIncomingMD(ctx)
	}
	opts := core.RedactOptions{
		Mode:                   msg.GetMode(),
		Operator:               msg.GetOperator(),
		MaskChar:               msg.GetMaskChar(),
		OCRLangs:               msg.GetOcrLangs(),
		Entities:               msg.GetEntities(),
		ScoreThreshold:         msg.GetScoreThreshold(),
		ScoreThresholdSet:      msg.GetScoreThresholdSet(),
		DPI:                    int(msg.GetDpi()),
		BoxPadding:             int(msg.GetBoxPadding()),
		DisableVisualHeuristic: msg.GetDisableVisualHeuristic(),
		DisableNER:             msg.GetDisableNer(),
	}
	id, redacted, stats, err := svc.RedactPDF(ctx, tenantID, msg.GetPdfContent(), opts)
	if err != nil {
		return nil, err
	}
	return &anondev1.AnonymizePDFResponse{
		Id:               id,
		TenantId:         tenantID,
		RedactedPdf:      redacted,
		EntitiesCount:    int32(stats.EntityCount),
		EntityTypesCount: int32(stats.TypeCount),
		EntitiesByType:   intMapToInt32Map(stats.ByType),
	}, nil
}

func executeRevealPDF(ctx context.Context, svc *core.Service, msg *anondev1.RevealPDFRequest) (*anondev1.RevealPDFResponse, error) {
	tenantID := msg.GetTenantId()
	if tenantID == "" {
		tenantID = tenantFromIncomingMD(ctx)
	}
	raw, err := svc.GetOriginalPDF(ctx, tenantID, msg.GetId())
	if err != nil {
		return nil, err
	}
	return &anondev1.RevealPDFResponse{
		Id:          msg.GetId(),
		OriginalPdf: raw,
	}, nil
}

func intMapToInt32Map(in map[string]int) map[string]int32 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int32, len(in))
	for k, v := range in {
		out[k] = int32(v)
	}
	return out
}

// applyAnalyzerOptions copies AnalyzerOptions from a proto request onto
// the existing internal-request fields. The internal types are reused
// verbatim so the Service layer stays unchanged.
//
// score_threshold_set is the explicit "field present" signal. Without
// it, score_threshold=0 is ambiguous between "include everything" and
// "use service default". The internal type's float64 zero is currently
// treated as "default" downstream — threading score_threshold_set into
// AnalysisConfig (or switching ScoreThreshold to *float64) is the way
// to honor an explicit 0 as "no filter".
func applyAnalyzerOptions(
	language *string,
	entities *[]string,
	scoreThreshold *float64,
	disableNER *bool,
	opts *anondev1.AnalyzerOptions,
) {
	if opts == nil {
		return
	}
	*language = opts.GetLanguage()
	*entities = opts.GetEntities()
	*disableNER = opts.GetDisableNer()
	if opts.GetScoreThresholdSet() {
		*scoreThreshold = opts.GetScoreThreshold()
	}
}

func findingsToProto(in []analyzer.RecognizerResult) []*anondev1.Finding {
	if len(in) == 0 {
		return nil
	}
	out := make([]*anondev1.Finding, len(in))
	for i, f := range in {
		out[i] = &anondev1.Finding{
			Start:          int32(f.Start),
			End:            int32(f.End),
			Score:          f.Score,
			EntityType:     f.EntityType,
			RecognizerName: f.RecognizerName,
		}
	}
	return out
}

func tokensToProto(in []core.TokenRef) []*anondev1.TokenRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]*anondev1.TokenRef, len(in))
	for i, t := range in {
		out[i] = &anondev1.TokenRef{
			Token:      t.Token,
			EntityType: t.EntityType,
			Start:      int32(t.Start),
			End:        int32(t.End),
		}
	}
	return out
}
