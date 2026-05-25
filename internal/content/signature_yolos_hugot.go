//go:build hugot

package content

import (
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Visual signature-detection via YOLOS — an open-source ViT-based
// DETR-style detector fine-tuned on Tobacco-800 signatures, ~248 MB
// INT8 ONNX published by onnx-community on HF.
//
// Architecture: input "pixel_values" [B,3,640,640] float32 in ImageNet
// normalisation. Outputs "logits" [B,Q,2] (signature, no-object) and
// "pred_boxes" [B,Q,4] in normalised cxcywh.
const (
	signatureModelURL = "https://huggingface.co/onnx-community/yolos-base-signature-detection-ONNX/resolve/main/onnx/model_int8.onnx"
	signatureInputSz  = 640
	// Default threshold tuned on scanned forms: 0.25 catches clear
	// handwritten signatures (typical conf >0.80) and dense graphic
	// elements like coat-of-arms / heraldic logos (~0.25-0.35).
	// Override via SIGNATURE_THRESHOLD env. Below 0.18 the model
	// starts firing on dense text blocks.
	signatureConfMin = 0.25
	signatureIOUMin  = 0.55
)

var (
	imagenetMean = [3]float32{0.485, 0.456, 0.406}
	imagenetStd  = [3]float32{0.229, 0.224, 0.225}

	signatureOrtOnce sync.Once
	signatureOrtErr  error
)

// LoadSignatureDetector downloads (on first use) the YOLOS
// signature-detection ONNX, initialises an in-process onnxruntime
// session, and returns a VisualDetector. Exported so both the CLI
// (cmd/anonymize-pdf) and the HTTP server (cmd/anonde) can wire
// it identically.
//
// overridePath, when non-empty, points at an existing local ONNX
// file and bypasses the download — used by Dockerfile.anonde-ner
// to bake the model at image-build time.
func LoadSignatureDetector(overridePath string) (VisualDetector, error) {
	ensureORTPath()

	modelPath := overridePath
	if modelPath == "" {
		var err error
		modelPath, err = cachedSignatureModelPath()
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		if overridePath != "" {
			return nil, fmt.Errorf("signature model %s not found", overridePath)
		}
		fmt.Fprintf(os.Stderr, "anonde: downloading signature model (~248 MB) to %s — first run only...\n", modelPath)
		if err := downloadFile(signatureModelURL, modelPath); err != nil {
			return nil, fmt.Errorf("download signature model: %w", err)
		}
	} else if err != nil {
		return nil, err
	}

	signatureOrtOnce.Do(func() {
		if libPath := os.Getenv("ORT_SO_PATH"); libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
		signatureOrtErr = ort.InitializeEnvironment()
	})
	if signatureOrtErr != nil && !isAlreadyInitErr(signatureOrtErr) {
		return nil, fmt.Errorf("signature: init ORT: %w", signatureOrtErr)
	}

	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{"pixel_values"},
		[]string{"logits", "pred_boxes"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("signature: open session: %w", err)
	}
	return &signatureDetector{session: session}, nil
}

func cachedSignatureModelPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cache", "anonde", "models", "signature")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "yolos-base-signature-int8.onnx"), nil
}

// ensureORTPath sets ORT_SO_PATH to a sensible default for the host
// platform when the caller hasn't picked one. yalue/onnxruntime_go
// fails loud if the path is wrong, so a wrong guess here is visible.
func ensureORTPath() {
	if os.Getenv("ORT_SO_PATH") != "" {
		return
	}
	candidates := []string{
		"/usr/lib/x86_64-linux-gnu/libonnxruntime.so.1",
		"/opt/homebrew/lib/libonnxruntime.dylib",
		"/usr/local/lib/libonnxruntime.dylib",
		"/usr/local/lib/libonnxruntime.so",
		"/usr/lib/libonnxruntime.so",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			os.Setenv("ORT_SO_PATH", p)
			return
		}
	}
}

func downloadFile(url, dst string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "anonde")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func isAlreadyInitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "initialized") || strings.Contains(msg, "Initialized")
}

type signatureDetector struct {
	session *ort.DynamicAdvancedSession
}

func (d *signatureDetector) Detect(img image.Image) ([]image.Rectangle, error) {
	confMin := float32(signatureConfMin)
	if v := os.Getenv("SIGNATURE_THRESHOLD"); v != "" {
		var f float64
		_, _ = fmt.Sscanf(v, "%f", &f)
		if f > 0 {
			confMin = float32(f)
		}
	}
	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()

	inputData := make([]float32, 3*signatureInputSz*signatureInputSz)
	plane := signatureInputSz * signatureInputSz
	for y := 0; y < signatureInputSz; y++ {
		srcY := bounds.Min.Y + (y*origH)/signatureInputSz
		for x := 0; x < signatureInputSz; x++ {
			srcX := bounds.Min.X + (x*origW)/signatureInputSz
			r, g, b, _ := img.At(srcX, srcY).RGBA()
			rN := float32(r>>8) / 255.0
			gN := float32(g>>8) / 255.0
			bN := float32(b>>8) / 255.0
			idx := y*signatureInputSz + x
			inputData[0*plane+idx] = (rN - imagenetMean[0]) / imagenetStd[0]
			inputData[1*plane+idx] = (gN - imagenetMean[1]) / imagenetStd[1]
			inputData[2*plane+idx] = (bN - imagenetMean[2]) / imagenetStd[2]
		}
	}

	inputTensor, err := ort.NewTensor(ort.NewShape(1, 3, signatureInputSz, signatureInputSz), inputData)
	if err != nil {
		return nil, fmt.Errorf("signature: input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputs := []ort.Value{nil, nil}
	if err := d.session.Run([]ort.Value{inputTensor}, outputs); err != nil {
		return nil, fmt.Errorf("signature: run: %w", err)
	}
	logits, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("signature: unexpected logits type %T", outputs[0])
	}
	defer logits.Destroy()
	boxes, ok := outputs[1].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("signature: unexpected boxes type %T", outputs[1])
	}
	defer boxes.Destroy()

	logitsShape := logits.GetShape()
	boxShape := boxes.GetShape()
	if len(logitsShape) != 3 || len(boxShape) != 3 ||
		logitsShape[0] != 1 || boxShape[0] != 1 ||
		logitsShape[2] < 2 || boxShape[2] != 4 {
		return nil, fmt.Errorf("signature: unexpected output shapes logits=%v boxes=%v", logitsShape, boxShape)
	}
	nQueries := int(logitsShape[1])
	nClassesPlus1 := int(logitsShape[2])
	logitData := logits.GetData()
	boxData := boxes.GetData()

	type det struct {
		rect image.Rectangle
		conf float32
	}
	var candidates []det
	for q := 0; q < nQueries; q++ {
		base := q * nClassesPlus1
		maxLogit := logitData[base]
		for c := 1; c < nClassesPlus1; c++ {
			if logitData[base+c] > maxLogit {
				maxLogit = logitData[base+c]
			}
		}
		var sumExp float32
		for c := 0; c < nClassesPlus1; c++ {
			sumExp += float32(math.Exp(float64(logitData[base+c] - maxLogit)))
		}
		sigProb := float32(math.Exp(float64(logitData[base+0]-maxLogit))) / sumExp
		if sigProb < confMin {
			continue
		}
		bbase := q * 4
		cx := boxData[bbase+0]
		cy := boxData[bbase+1]
		bw := boxData[bbase+2]
		bh := boxData[bbase+3]
		x0 := int(float64(cx-bw/2) * float64(origW))
		y0 := int(float64(cy-bh/2) * float64(origH))
		x1 := int(float64(cx+bw/2) * float64(origW))
		y1 := int(float64(cy+bh/2) * float64(origH))
		rect := image.Rect(x0, y0, x1, y1).Intersect(bounds)
		if rect.Empty() {
			continue
		}
		candidates = append(candidates, det{rect: rect, conf: sigProb})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].conf > candidates[j].conf })
	kept := candidates[:0]
	for _, c := range candidates {
		drop := false
		for _, k := range kept {
			if iou(c.rect, k.rect) > signatureIOUMin {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, c)
		}
	}
	rects := make([]image.Rectangle, len(kept))
	for i, k := range kept {
		rects[i] = k.rect
	}
	return rects, nil
}

func iou(a, b image.Rectangle) float64 {
	inter := a.Intersect(b)
	if inter.Empty() {
		return 0
	}
	ai := area(a)
	bi := area(b)
	ii := area(inter)
	return float64(ii) / float64(ai+bi-ii)
}

func area(r image.Rectangle) int { return r.Dx() * r.Dy() }
