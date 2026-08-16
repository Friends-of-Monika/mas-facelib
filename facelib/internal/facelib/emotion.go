package facelib

import (
	"errors"
	"image"
	"math"
	"sync"

	onnx "github.com/owulveryck/onnx-go"
	"github.com/owulveryck/onnx-go/backend/x/gorgonnx"
	"gorgonia.org/tensor"

	"github.com/friends-of-monika/mas-facelib/facelib/internal/data"
)

// EmotionInputSize is the width and height the emotion model expects.
const EmotionInputSize = 64

// NumEmotions is the number of classes the emotion model scores.
const NumEmotions = len(data.EmotionLabels)

// Scores holds the per-class emotion probabilities
// indexed to match data.EmotionLabels.
type Scores [NumEmotions]float32

var (
	ErrModelLoad  = errors.New("facelib: could not load emotion model")
	ErrInference  = errors.New("facelib: emotion inference failed")
	ErrOutputSize = errors.New("facelib: emotion model returned unexpected output shape")
)

type emotionEngine struct {
	// mu serializes inference; the gorgonnx graph carries per-node state
	// across Run(), so two concurrent calls would corrupt each other's
	// intermediate values
	mu      sync.Mutex
	model   *onnx.Model
	backend *gorgonnx.Graph
	input   tensor.Tensor
}

var (
	engine     *emotionEngine
	engineErr  error
	engineOnce sync.Once
)

// loadEngine decodes the embedded model exactly once. This costs roughly ~100 ms
// and a comparable amount of heap, so it is deliberately deferred until a face is
// actually found rather than at library load time.
func loadEngine() (*emotionEngine, error) {
	engineOnce.Do(func() {
		backend := gorgonnx.NewGraph()
		model := onnx.NewModel(backend)
		if err := model.UnmarshalBinary(data.EmotionModel); err != nil {
			engineErr = errors.Join(ErrModelLoad, err)
			return
		}
		engine = &emotionEngine{
			model:   model,
			backend: backend,
			input: tensor.New(
				tensor.WithShape(1, 1, EmotionInputSize, EmotionInputSize),
				tensor.Of(tensor.Float32),
			),
		}
	})
	return engine, engineErr
}

// PreloadEmotionModel forces the model to be decoded now, so that callers that care
// about latency on the first real frame can handle the decode cost upfront instead.
func PreloadEmotionModel() error {
	_, err := loadEngine()
	return err
}

// ClassifyEmotion scores a 64x64 grayscale face crop.
//
// The model normalizes its own input, so pixels are handed over as raw 0-255
// values widened to float32 with no scaling applied here.
func ClassifyEmotion(face *image.Gray) (Scores, error) {
	var out Scores

	b := face.Bounds()
	if b.Dx() != EmotionInputSize || b.Dy() != EmotionInputSize {
		return out, ErrOutputSize
	}

	e, err := loadEngine()
	if err != nil {
		return out, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Walk row by row via Stride rather than over Pix directly: a sub-image
	// view has padding between rows, and reading it flat would shear the face.
	buf := e.input.Data().([]float32)
	for y := range EmotionInputSize {
		row := face.Pix[face.PixOffset(b.Min.X, b.Min.Y+y):]
		dst := buf[y*EmotionInputSize:]
		for x := range EmotionInputSize {
			dst[x] = float32(row[x])
		}
	}

	if err := e.model.SetInput(0, e.input); err != nil {
		return out, errors.Join(ErrInference, err)
	}
	if err := e.backend.Run(); err != nil {
		return out, errors.Join(ErrInference, err)
	}

	res, err := e.model.GetOutputTensors()
	if err != nil {
		return out, errors.Join(ErrInference, err)
	}
	if len(res) == 0 {
		return out, ErrOutputSize
	}

	logits, ok := res[0].Data().([]float32)
	if !ok || len(logits) != NumEmotions {
		return out, ErrOutputSize
	}

	return softmax(logits), nil
}

// softmax converts the model's raw logits into probabilities.
func softmax(logits []float32) Scores {
	var out Scores

	maxLogit := logits[0]
	for _, v := range logits[1:] {
		if v > maxLogit {
			maxLogit = v
		}
	}

	var sum float64
	exps := make([]float64, len(logits))
	for i, v := range logits {
		// the maximum is subtracted first so that large logits
		// cannot overflow the exponential
		e := math.Exp(float64(v - maxLogit))
		exps[i] = e
		sum += e
	}

	for i, e := range exps {
		out[i] = float32(e / sum)
	}

	return out
}

// Top returns the index and probability of the highest-scoring emotion.
func (s Scores) Top() (int, float32) {
	best := 0
	for i := 1; i < len(s); i++ {
		if s[i] > s[best] {
			best = i
		}
	}
	return best, s[best]
}
