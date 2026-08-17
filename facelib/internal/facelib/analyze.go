package facelib

import (
	"fmt"
	"image"
	"os"

	_ "image/jpeg"
	_ "image/png"

	"github.com/friends-of-monika/mas-facelib/facelib/internal/data"
)

// Options controls a single AnalyzeData call.
type Options struct {
	// Detect provides face detection parameters.
	Detect DetectParams

	// Emotion enables emotion classification. When false, AnalyzeData only
	// reports face boxes and never touches the ONNX model.
	Emotion bool

	// MaxFaces caps how many faces are emotion-classified, strongest first.
	// Every detected face is still reported, zero or less means all of them.
	MaxFaces int

	// Padding expands each face box by this fraction of its size before
	// cropping for the emotion model, e.g. 0.1 grows the box by 10%.
	// Negative values tighten it.
	Padding float64
}

// DefaultOptions returns the settings used when the caller passes none.
func DefaultOptions() Options {
	return Options{
		Detect:   DefaultDetectParams(),
		Emotion:  true,
		MaxFaces: 1,
		Padding:  0.0,
	}
}

// FaceResult describes one detected face.
type FaceResult struct {
	// Face rectangle coordinates.
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`

	// Quality is the cascade's raw confidence. It is unbounded and not a
	// probability; it is only meaningful relative to other detections.
	Quality float32 `json:"quality"`

	// Emotion is the highest-scoring class, empty if not classified.
	Emotion string `json:"emotion,omitempty"`
	// Confidence is the probability of Emotion, zero if not classified.
	Confidence float32 `json:"confidence,omitempty"`
	// Scores maps every emotion label to its probability, nil if not
	// classified. Values sum to 1.
	Scores map[string]float32 `json:"scores,omitempty"`
	// EmotionError explains why this face could not be classified, if the
	// detection itself succeeded but classification did not.
	EmotionError string `json:"emotion_error,omitempty"`
}

// Result is the outcome of one AnalyzeData call.
type Result struct {
	// Face reports whether any face was detected at all.
	Face bool `json:"face"`
	// Count reports found faces count.
	Count int `json:"count"`
	// Faces presents an array of detected faces.
	Faces []FaceResult `json:"faces"`
}

// AnalyzeData detects faces in a raw pixel buffer and optionally classifies their emotions.
//
// buf is only read during the call and is never retained.
func AnalyzeData(buf []uint8, width, height, stride, channels int, opts Options) (*Result, error) {
	g, err := GrayFromBuffer(buf, width, height, stride, channels)
	if err != nil {
		return nil, err
	}
	return analyzeGray(g, opts)
}

// AnalyzeFile decodes a PNG or JPEG from disk and analyzes it.
func AnalyzeFile(path string, opts Options) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("facelib: could not decode %s: %w", path, err)
	}
	return analyzeGray(GrayFromImage(src), opts)
}

// analyzeGray runs detection and classification over an already grayscale
// frame, shared by every entry point.
func analyzeGray(g *image.Gray, opts Options) (*Result, error) {
	faces, err := Detect(g, opts.Detect)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Face:  len(faces) > 0,
		Count: len(faces),
		Faces: make([]FaceResult, 0, len(faces)),
	}

	limit := opts.MaxFaces
	if limit <= 0 {
		limit = len(faces)
	}

	for i, f := range faces {
		fr := FaceResult{
			X: f.Rect.X, Y: f.Rect.Y, W: f.Rect.W, H: f.Rect.H,
			Quality: f.Quality,
		}
		if opts.Emotion && i < limit {
			scores, err := classifyFace(g, f.Rect, opts.Padding)
			if err != nil {
				// A classification failure on one face is not fatal:
				// the detection result is still worth returning
				fr.EmotionError = err.Error()
			} else {
				top, conf := scores.Top()
				fr.Emotion = data.EmotionLabels[top]
				fr.Confidence = conf
				fr.Scores = make(map[string]float32, NumEmotions)
				for j, label := range data.EmotionLabels {
					fr.Scores[label] = scores[j]
				}
			}
		}
		res.Faces = append(res.Faces, fr)
	}

	return res, nil
}

// classifyFace crops, pads and resamples a face box, then scores it.
func classifyFace(g *image.Gray, r Rect, padding float64) (Scores, error) {
	var zero Scores

	if padding != 0 {
		dx := int(float64(r.W) * padding / 2)
		dy := int(float64(r.H) * padding / 2)
		r = Rect{X: r.X - dx, Y: r.Y - dy, W: r.W + 2*dx, H: r.H + 2*dy}
	}

	crop, err := Crop(g, r)
	if err != nil {
		return zero, err
	}
	resized, err := Resize(crop, EmotionInputSize, EmotionInputSize)
	if err != nil {
		return zero, err
	}
	return ClassifyEmotion(resized)
}
