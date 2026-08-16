package facelib

import (
	"errors"
	"image"
	"sort"
	"sync"

	pigo "github.com/esimov/pigo/core"

	"github.com/friends-of-monika/mas-facelib/facelib/internal/data"
)

// Face is a single detected face.
type Face struct {
	Rect    Rect
	Quality float32
}

// DetectParams tunes the face detector.
type DetectParams struct {
	// MinSize and MaxSize bound the face size in pixels.
	MinSize, MaxSize int
	// ShiftFactor is the detection window step, as a fraction of window size.
	ShiftFactor float64
	// ScaleFactor is the multiplier between successive window sizes.
	ScaleFactor float64
	// IoUThreshold merges overlapping detections.
	IoUThreshold float64
	// QualityThreshold discards detections the cascade is not confident about.
	// The facefinder cascade's scores are unbounded; 5.0 is the value pigo's
	// own tooling uses and rejects most false positives.
	QualityThreshold float32
}

// DefaultDetectParams returns detector settings tuned for a single, roughly
// front-facing subject filling a good part of the frame, which is the webcam
// case this library exists for.
func DefaultDetectParams() DetectParams {
	return DetectParams{
		MinSize:          60,
		MaxSize:          1000,
		ShiftFactor:      0.15,
		ScaleFactor:      1.1,
		IoUThreshold:     0.2,
		QualityThreshold: 5.0,
	}
}

var (
	classifier     *pigo.Pigo
	classifierErr  error
	classifierOnce sync.Once
)

// loadClassifier unpacks the embedded cascade exactly once. Unpacking costs a
// few milliseconds and the result is immutable, so it is shared across calls.
func loadClassifier() (*pigo.Pigo, error) {
	classifierOnce.Do(func() {
		classifier, classifierErr = pigo.NewPigo().Unpack(data.FaceFinder)
	})
	return classifier, classifierErr
}

// ErrNoClassifier reports an unusable embedded cascade.
var ErrNoClassifier = errors.New("facelib: could not unpack face cascade")

// Detect finds faces in g, strongest first.
func Detect(g *image.Gray, p DetectParams) ([]Face, error) {
	c, err := loadClassifier()
	if err != nil {
		return nil, errors.Join(ErrNoClassifier, err)
	}

	b := g.Bounds()
	width, height := b.Dx(), b.Dy()

	// MaxSize larger than the image wastes no work in pigo, but clamping it
	// keeps the reported pyramid honest for small frames.
	maxSize := p.MaxSize
	if limit := min(width, height); maxSize > limit {
		maxSize = limit
	}
	if p.MinSize > maxSize {
		return nil, nil
	}

	params := pigo.CascadeParams{
		MinSize:     p.MinSize,
		MaxSize:     maxSize,
		ShiftFactor: p.ShiftFactor,
		ScaleFactor: p.ScaleFactor,
		ImageParams: pigo.ImageParams{
			// Dim is pigo's row stride. Passing g.Stride rather than the width
			// means a sub-image view works here without being repacked.
			Pixels: g.Pix[g.PixOffset(b.Min.X, b.Min.Y):],
			Rows:   height,
			Cols:   width,
			Dim:    g.Stride,
		},
	}

	// Angle 0: upright faces only. Rotated cascades cost a full extra pass
	// each and buy nothing for a subject sitting in front of a webcam.
	dets := c.RunCascade(params, 0.0)
	dets = c.ClusterDetections(dets, p.IoUThreshold)

	faces := make([]Face, 0, len(dets))
	for _, d := range dets {
		if d.Q < p.QualityThreshold {
			continue
		}

		// pigo reports a center point and a window size, not a corner.
		faces = append(faces, Face{
			Rect: Rect{
				X: d.Col - d.Scale/2,
				Y: d.Row - d.Scale/2,
				W: d.Scale,
				H: d.Scale,
			},
			Quality: d.Q,
		})
	}

	sort.SliceStable(faces, func(i, j int) bool {
		return faces[i].Quality > faces[j].Quality
	})

	return faces, nil
}
