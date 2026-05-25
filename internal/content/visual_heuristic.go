package content

import (
	"image"
	"image/color"
)

// detectVisualPIIHeuristic finds page regions that look like ink (high
// dark-pixel density) but aren't covered by a confidently OCR'd word.
// Returns axis-aligned rectangles in page-image pixel coordinates that
// the caller should treat as candidate signatures / logos / stamps /
// scribbles and overlay with the standard redaction fill.
//
// Algorithm (no ML, no extra deps):
//
//  1. Tile the page into TileSize-px squares.
//  2. For each tile, count "dark" pixels (luma < darkThreshold). Tiles
//     with density above tileDensityFloor are flagged as "inky."
//  3. Subtract tiles that overlap a HIGH-confidence OCR word — those
//     are just printed text the analyzer is already handling via the
//     word-bbox path. Confidence threshold is configurable
//     (textConfFloor) so OCR-jitter on legitimate text doesn't fall
//     into the visual lane.
//  4. Connected-component label the remaining tiles (4-connectivity).
//  5. Drop components smaller than minComponentTiles (eliminates a
//     single stray inky tile from OCR misses) and bounding boxes
//     thinner than minBoxHeight px (eliminates underline rules).
//  6. Return the bounding box of each surviving component.
//
// Trade-offs: this catches handwritten signatures, ink stamps, dense
// logos, and barcode-like shapes. It MISSES logos that are mostly
// white-space with a thin outline. It can FALSE-POSITIVE on tables
// with heavy borders. Both classes are acceptable for a redaction
// tool — over-redacting a table border costs nothing; missing a
// signature leaks PII. Tunable via VisualHeuristicOptions.
type VisualHeuristicOptions struct {
	TileSize          int     // pixels per square tile (default 24)
	DarkThreshold     uint8   // luma threshold for "dark" (default 120)
	TileDensityFloor  float64 // fraction of dark pixels needed to flag a tile (default 0.10)
	TextConfFloor     float64 // tesseract conf above which a word is treated as printed text (default 55)
	MinComponentTiles int     // smallest connected blob worth reporting (default 3)
	MinBoxHeight      int     // pixels — drops thin horizontal rules (default 16)
	// MaxBoxAreaFrac caps the size of any single emitted box as a
	// fraction of the page area. Components above this fraction are
	// almost always false positives from tables / paragraphs whose
	// rows happen to bridge into one connected component. Default
	// 0.08 (8% of the page).
	MaxBoxAreaFrac    float64
	// MaxBoxes is a hard cap on the number of boxes emitted per page.
	// Beyond this point the heuristic is over-firing; better to drop
	// the noise than carpet-bomb the page. Default 6.
	MaxBoxes          int
}

func defaultVisualOpts() VisualHeuristicOptions {
	// Tuned against scanned government forms (the target corpus —
	// Romanian garnishment notices, German clinical letters, English
	// contracts). Handwritten signatures are sparse ink at the page
	// edges; the floor needs to be low enough to catch them but high
	// enough to ignore OCR's salt-and-pepper noise on white background.
	return VisualHeuristicOptions{
		TileSize:          24,
		DarkThreshold:     120,
		TileDensityFloor:  0.08,
		TextConfFloor:     55,
		MinComponentTiles: 3,
		MinBoxHeight:      16,
		MaxBoxAreaFrac:    0.08,
		MaxBoxes:          6,
	}
}

// isLikelyTextOnlyPage cheaply classifies whether a page is text-only
// — i.e. has no signature / logo / stamp / face / barcode that the
// vision detector would ever fire on. Callers use this to short-
// circuit the ~500 ms YOLOS inference on born-digital text PDFs and
// the many text-only pages in marketing decks.
//
// Method: reuse the tile-density map from the visual heuristic.
// Count tiles that are dark AND not overlapping any confident OCR
// word. Zero or near-zero such tiles → no non-text ink → vision can't
// find anything. Threshold is small (3 tiles ≈ 72×72 px at 24-px
// tile size) so a single OCR stray won't keep vision running on
// every page.
//
// Cost: ~5-15 ms/page at 200 DPI. Vision inference is ~500 ms/page,
// so this pays back on the first text-only page.
func isLikelyTextOnlyPage(img image.Image, words []OCRWord, opts VisualHeuristicOptions) bool {
	if opts.TileSize == 0 {
		opts = defaultVisualOpts()
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	tilesX := (width + opts.TileSize - 1) / opts.TileSize
	tilesY := (height + opts.TileSize - 1) / opts.TileSize
	if tilesX == 0 || tilesY == 0 {
		return true
	}

	// Quick text-only proof: if we have a reasonable amount of OCR
	// words AND essentially no inky tiles outside word boxes, vision
	// has nothing to find.
	suspicious := 0
	for ty := 0; ty < tilesY; ty++ {
		y0 := ty * opts.TileSize
		y1 := y0 + opts.TileSize
		if y1 > height {
			y1 = height
		}
		for tx := 0; tx < tilesX; tx++ {
			x0 := tx * opts.TileSize
			x1 := x0 + opts.TileSize
			if x1 > width {
				x1 = width
			}
			dark, total := 0, 0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					if isDark(img.At(bounds.Min.X+x, bounds.Min.Y+y), opts.DarkThreshold) {
						dark++
					}
					total++
				}
			}
			if total == 0 {
				continue
			}
			if float64(dark)/float64(total) < opts.TileDensityFloor {
				continue
			}
			// Tile is inky. Check if covered by any confident OCR word.
			covered := false
			for _, w := range words {
				if w.Conf < opts.TextConfFloor {
					continue
				}
				wtx0 := w.Left / opts.TileSize
				wty0 := w.Top / opts.TileSize
				wtx1 := (w.Left + w.Width) / opts.TileSize
				wty1 := (w.Top + w.Heigh) / opts.TileSize
				if tx >= wtx0 && tx <= wtx1 && ty >= wty0 && ty <= wty1 {
					covered = true
					break
				}
			}
			if !covered {
				suspicious++
				if suspicious >= 3 {
					return false
				}
			}
		}
	}
	return suspicious < 3
}

func detectVisualPIIHeuristic(img image.Image, words []OCRWord, opts VisualHeuristicOptions) []image.Rectangle {
	if opts.TileSize == 0 {
		opts = defaultVisualOpts()
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	tilesX := (width + opts.TileSize - 1) / opts.TileSize
	tilesY := (height + opts.TileSize - 1) / opts.TileSize
	if tilesX == 0 || tilesY == 0 {
		return nil
	}

	// Step 1+2: tile density map. inky[y*tilesX + x] = true if dense.
	inky := make([]bool, tilesX*tilesY)
	for ty := 0; ty < tilesY; ty++ {
		y0 := ty * opts.TileSize
		y1 := y0 + opts.TileSize
		if y1 > height {
			y1 = height
		}
		for tx := 0; tx < tilesX; tx++ {
			x0 := tx * opts.TileSize
			x1 := x0 + opts.TileSize
			if x1 > width {
				x1 = width
			}
			dark, total := 0, 0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					if isDark(img.At(bounds.Min.X+x, bounds.Min.Y+y), opts.DarkThreshold) {
						dark++
					}
					total++
				}
			}
			if total == 0 {
				continue
			}
			if float64(dark)/float64(total) >= opts.TileDensityFloor {
				inky[ty*tilesX+tx] = true
			}
		}
	}

	// Step 3: subtract tiles overlapping confident OCR words.
	for _, w := range words {
		if w.Conf < opts.TextConfFloor {
			continue
		}
		tx0 := w.Left / opts.TileSize
		ty0 := w.Top / opts.TileSize
		tx1 := (w.Left + w.Width) / opts.TileSize
		ty1 := (w.Top + w.Heigh) / opts.TileSize
		for ty := ty0; ty <= ty1; ty++ {
			if ty < 0 || ty >= tilesY {
				continue
			}
			for tx := tx0; tx <= tx1; tx++ {
				if tx < 0 || tx >= tilesX {
					continue
				}
				inky[ty*tilesX+tx] = false
			}
		}
	}

	// Step 4: connected-component labeling (BFS, 4-connectivity).
	labels := make([]int, len(inky))
	curLabel := 0
	components := map[int][]image.Point{}
	for i := range inky {
		if !inky[i] || labels[i] != 0 {
			continue
		}
		curLabel++
		queue := []int{i}
		labels[i] = curLabel
		for len(queue) > 0 {
			j := queue[0]
			queue = queue[1:]
			x, y := j%tilesX, j/tilesX
			components[curLabel] = append(components[curLabel], image.Point{X: x, Y: y})
			for _, d := range []image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d.X, y+d.Y
				if nx < 0 || nx >= tilesX || ny < 0 || ny >= tilesY {
					continue
				}
				k := ny*tilesX + nx
				if inky[k] && labels[k] == 0 {
					labels[k] = curLabel
					queue = append(queue, k)
				}
			}
		}
	}

	// Step 5+6: emit bounding rects of large enough components.
	pageArea := width * height
	maxAllowed := int(opts.MaxBoxAreaFrac * float64(pageArea))
	if maxAllowed <= 0 {
		maxAllowed = pageArea // disabled
	}
	type sized struct {
		rect image.Rectangle
		area int
	}
	var keep []sized
	for _, comp := range components {
		if len(comp) < opts.MinComponentTiles {
			continue
		}
		minX, minY := comp[0].X, comp[0].Y
		maxX, maxY := comp[0].X, comp[0].Y
		for _, p := range comp[1:] {
			if p.X < minX {
				minX = p.X
			}
			if p.X > maxX {
				maxX = p.X
			}
			if p.Y < minY {
				minY = p.Y
			}
			if p.Y > maxY {
				maxY = p.Y
			}
		}
		rect := image.Rect(
			minX*opts.TileSize,
			minY*opts.TileSize,
			(maxX+1)*opts.TileSize,
			(maxY+1)*opts.TileSize,
		).Intersect(bounds)
		if rect.Dy() < opts.MinBoxHeight {
			continue
		}
		area := rect.Dx() * rect.Dy()
		if area > maxAllowed {
			// Almost always a paragraph or table block — skip.
			continue
		}
		keep = append(keep, sized{rect: rect, area: area})
	}
	// Cap total boxes: prefer the smaller, denser blobs (more likely
	// signatures and logos; larger blobs are more likely false-
	// positive layout regions).
	if opts.MaxBoxes > 0 && len(keep) > opts.MaxBoxes {
		// Stable partial sort: smallest area first.
		for i := 1; i < len(keep); i++ {
			for j := i; j > 0 && keep[j].area < keep[j-1].area; j-- {
				keep[j], keep[j-1] = keep[j-1], keep[j]
			}
		}
		keep = keep[:opts.MaxBoxes]
	}
	rects := make([]image.Rectangle, len(keep))
	for i, k := range keep {
		rects[i] = k.rect
	}
	return rects
}

func isDark(c color.Color, threshold uint8) bool {
	r, g, b, _ := c.RGBA()
	luma := uint32(0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8))
	return luma < uint32(threshold)
}
