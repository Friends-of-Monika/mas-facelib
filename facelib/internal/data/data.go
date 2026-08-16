// Package data holds the binary models baked into the shared library.
// Note: we don't ship any models in-tree, so before you build you should
// run the scripts/fetch-assets.sh script which will download them for you.
package data

import _ "embed"

// FaceFinder is the pigo face detection cascade.
// Source: github.com/esimov/pigo, cascade/facefinder (MIT)
//
//go:embed facefinder
var FaceFinder []byte

// EmotionModel is the ONNX FER+ emotion recognition model.
//
// Source: github.com/onnx/models, emotion_ferplus/model/emotion-ferplus-8.onnx (MIT)
// Input "Input3" is a (1, 1, 64, 64) float32 tensor of raw 0-255 grayscale
// values; the graph applies its own mean/scale normalization. The output is 8 unnormalized
// logits ordered as EmotionLabels.
//
//go:embed emotion-ferplus-8.onnx
var EmotionModel []byte

// EmotionLabels names the emotion model output classes, in output order.
var EmotionLabels = [8]string{
	"neutral",
	"happiness",
	"surprise",
	"sadness",
	"anger",
	"disgust",
	"fear",
	"contempt",
}
