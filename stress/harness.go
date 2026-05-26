//go:build stress

// Package stress is the load + edge-case tier that runs against a real
// production-shape container (every variant of the anonde Docker image).
// Gated by `-tags stress` so it never runs as part of `go test ./...`;
// stress requires Docker and is heavy enough that it belongs behind an
// explicit opt-in.
//
// Why containers instead of an in-process boot like the e2e tier:
//
//   - A pool-saturation or OOM test only proves anything against the
//     actual production artefact — env defaults, baked models, glibc
//     base image, CGO + libonnxruntime, tesseract + poppler, the works.
//     In-process Go boot misses most of that.
//   - testcontainers-go consumes the existing Dockerfile.anonde{,-ner,
//     -ner-stack} unchanged — same image a self-hoster would pull.
//   - One harness, three variants, comparable metrics.
//
// What this file is NOT: a tuning oracle. The thresholds in
// stress_test.go are pass/fail guards against regressions, not
// optimisation targets. Real perf work belongs in microbench/.
package stress

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Variant is one anonde Docker image. The stress matrix runs (most)
// cases against every Variant; tests that depend on a feature only
// the NER builds have (the PDF redactor, GLiNER pools) skip the
// patterns row via the HasNER / HasPDF flags.
type Variant struct {
	Name       string            // Subtest label and image tag suffix.
	Dockerfile string            // Repo-relative path. Build context = repo root.
	Env        map[string]string // Container env overrides on top of the Dockerfile defaults.
	HasNER     bool              // Pool / NER-dependent cases gate on this.
	HasPDF     bool              // PDF redaction cases (the raw-bytes endpoint) gate on this.
	// BuildTimeout caps the FromDockerfile build. NER variants pull
	// 770 MB+ of model weights at build time and can run 5-10 min cold
	// on a slow link; patterns is sub-30s.
	BuildTimeout time.Duration
}

// Variants is the full matrix. Tests iterate via stress.AllVariants()
// or, when only NER is relevant, stress.NERVariants().
var Variants = []Variant{
	{
		Name:         "patterns",
		Dockerfile:   "Dockerfile.anonde",
		Env:          map[string]string{"ANALYZER_BACKEND": "patterns"},
		HasNER:       false,
		HasPDF:       false,
		BuildTimeout: 5 * time.Minute,
	},
	{
		Name:       "ner",
		Dockerfile: "Dockerfile.anonde-ner",
		Env: map[string]string{
			// Default pool of 1 in the image; lift to 2 so the
			// pool-saturation test has something interesting to do
			// without making the test variant-aware.
			"GLINER_POOL_SIZE":              "2",
			"ANONDE_MAX_CONCURRENT_REQUESTS": "4",
		},
		HasNER:       true,
		HasPDF:       true,
		BuildTimeout: 15 * time.Minute,
	},
	{
		Name:       "ner-stack",
		Dockerfile: "Dockerfile.anonde-ner-stack",
		Env: map[string]string{
			"GLINER_POOL_SIZE":               "2",
			"ANONDE_GLINER_FLAT_POOL_SIZE":   "1",
			"ANONDE_MAX_CONCURRENT_REQUESTS": "3",
		},
		HasNER:       true,
		HasPDF:       true,
		BuildTimeout: 20 * time.Minute,
	},
}

// AllVariants returns the full matrix.
func AllVariants() []Variant { return Variants }

// NERVariants returns just the variants that have an NER backend (for
// pool / PDF tests).
func NERVariants() []Variant {
	out := make([]Variant, 0, len(Variants))
	for _, v := range Variants {
		if v.HasNER {
			out = append(out, v)
		}
	}
	return out
}

// Container wraps a running anonde container with the URLs the test
// code needs. The struct stays small on purpose — anything fancier
// (logs, exec, restart) goes through the embedded testcontainers
// handle.
type Container struct {
	Variant    Variant
	HTTPURL    string // http://host:port — REST + Connect + gRPC
	MetricsURL string // http://host:metricsPort/metrics
	inst       testcontainers.Container
}

// Start builds the variant's Dockerfile (cached by Docker between
// runs), boots a container, waits for /v1/health, and returns a
// Container ready to drive. Caller is responsible for Stop() — usually
// via t.Cleanup.
func Start(ctx context.Context, t *testing.T, v Variant) *Container {
	t.Helper()

	if testing.Short() {
		t.Skipf("stress: skipping %s in -short mode", v.Name)
	}

	root := repoRoot(t)

	// Dockerfile.anonde-ner{,-stack} use `FROM --platform=$TARGETPLATFORM`
	// which buildx sets automatically but the legacy `docker build`
	// path testcontainers-go uses leaves empty — that crashes parse
	// with `"" is an invalid OS component`. Pass the build args
	// explicitly so the multi-arch Dockerfiles work under plain docker.
	platform, arch := dockerBuildPlatform()
	buildArgs := map[string]*string{
		"TARGETPLATFORM": &platform,
		"TARGETARCH":     &arch,
	}

	// FromDockerfile rebuilds on every test boot unless Docker layer
	// cache hits. That's intentional: stress runs locally + on a
	// scheduled CI workflow, and we want it to fail loudly when the
	// Dockerfile drifts away from what self-hosters actually build.
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    root,
			Dockerfile: v.Dockerfile,
			BuildArgs:  buildArgs,
			KeepImage:  true,
			// PrintBuildLog is loud but the alternative is a silent
			// 15-minute hang on a stale `docker pull`. Loud wins.
			PrintBuildLog: true,
		},
		ExposedPorts: []string{"8080/tcp", "9090/tcp"},
		Env:          mergeEnv(map[string]string{"METRICS_BIND": "0.0.0.0:9090"}, v.Env),
		// /v1/health is the proto-defined liveness check; HTTP 200
		// means routes are wired + analyzer engine constructed.
		// Long startup window covers cold NER model load on the
		// stack variant.
		WaitingFor: wait.ForHTTP("/v1/health").
			WithPort("8080/tcp").
			WithStartupTimeout(3 * time.Minute),
	}

	startCtx, cancel := context.WithTimeout(ctx, v.BuildTimeout)
	defer cancel()
	inst, err := testcontainers.GenericContainer(startCtx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("stress[%s]: container start: %v", v.Name, err)
	}

	host, err := inst.Host(ctx)
	if err != nil {
		t.Fatalf("stress[%s]: host: %v", v.Name, err)
	}
	httpPort, err := inst.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("stress[%s]: http port: %v", v.Name, err)
	}
	metricsPort, err := inst.MappedPort(ctx, "9090/tcp")
	if err != nil {
		t.Fatalf("stress[%s]: metrics port: %v", v.Name, err)
	}

	c := &Container{
		Variant:    v,
		HTTPURL:    fmt.Sprintf("http://%s:%s", host, httpPort.Port()),
		MetricsURL: fmt.Sprintf("http://%s:%s/metrics", host, metricsPort.Port()),
		inst:       inst,
	}

	// Second-line readiness: a 200 from /v1/version proves the
	// service is fully constructed (not just listening). Cheap.
	if err := waitForVersion(ctx, c.HTTPURL); err != nil {
		c.Stop(ctx)
		t.Fatalf("stress[%s]: /v1/version not ready: %v", v.Name, err)
	}
	return c
}

// Stop terminates the container. Safe to call multiple times.
func (c *Container) Stop(ctx context.Context) {
	if c == nil || c.inst == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_ = c.inst.Terminate(stopCtx)
	c.inst = nil
}

func waitForVersion(ctx context.Context, baseURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/version", nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("/v1/version did not return 200 within 60s")
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// repoRoot finds the repo root by walking up from this file's location.
// Used as the Docker build context so the Dockerfile.* paths resolve
// regardless of where `go test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("stress: cannot resolve harness file path")
	}
	// stress/harness.go → repo root
	return filepath.Dir(filepath.Dir(file))
}

func mergeEnv(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// dockerBuildPlatform returns the `TARGETPLATFORM` / `TARGETARCH` pair
// to inject as docker build args. testcontainers-go drives the legacy
// `docker build`, which (unlike buildx) leaves these empty when the
// Dockerfile uses `FROM --platform=$TARGETPLATFORM ...`. anonde's
// NER images do, so we map the runtime arch to the Docker arch
// shorthand and pass them explicitly. amd64 + arm64 are the only
// arches the production Dockerfiles handle (see the case-switch in
// Dockerfile.anonde-ner); fall back to amd64 otherwise so the build
// still gets a useful error from the Dockerfile rather than a parse
// crash inside testcontainers.
func dockerBuildPlatform() (platform, arch string) {
	switch runtime.GOARCH {
	case "arm64":
		return "linux/arm64", "arm64"
	default:
		return "linux/amd64", "amd64"
	}
}

// ForEachVariant runs fn as a subtest for every variant in vs. The
// per-variant container is created in fn, not here, because some tests
// need to set extra env (e.g. tighter MAX_CONTENT_BYTES) before boot.
func ForEachVariant(t *testing.T, vs []Variant, fn func(*testing.T, Variant)) {
	t.Helper()
	for _, v := range vs {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			fn(t, v)
		})
	}
}

