// facelib is a shared library that detects faces and classifies
// their emotion, designed to interoperate with Python.
// Every entry point takes and returns C primitives and JSON.
package main

// #include <stdlib.h>
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"

	"github.com/friends-of-monika/mas-facelib/facelib/internal/facelib"
)

// version identifies the build
var version = "devel"

func main() {}

// apiResponse is the JSON envelope every call returns.
// Errors are passed in the same envelope rather than through a status
// code so that a ctypes caller only has to parse one thing.
type apiResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Face  bool                 `json:"face"`
	Count int                  `json:"count"`
	Faces []facelib.FaceResult `json:"faces,omitempty"`
}

// options mirrors facelib.Options as optional JSON fields.
// Omitted values become defaults instead of zeroes.
type options struct {
	Emotion          *bool    `json:"emotion"`
	MaxFaces         *int     `json:"max_faces"`
	Padding          *float64 `json:"padding"`
	MinSize          *int     `json:"min_size"`
	MaxSize          *int     `json:"max_size"`
	ShiftFactor      *float64 `json:"shift_factor"`
	ScaleFactor      *float64 `json:"scale_factor"`
	IoUThreshold     *float64 `json:"iou_threshold"`
	QualityThreshold *float32 `json:"quality_threshold"`
}

// parseOptions turns an optional JSON string from C into analysis options,
// falling back to the defaults when it is NULL or empty.
func parseOptions(opts_json *C.char) (facelib.Options, error) {
	opts := facelib.DefaultOptions()
	if opts_json == nil {
		return opts, nil
	}

	raw := C.GoString(opts_json)
	if raw == "" {
		return opts, nil
	}

	var o options
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return opts, err
	}
	return o.apply(opts), nil
}

// apply overlays the caller's overrides onto the defaults.
func (o *options) apply(base facelib.Options) facelib.Options {
	if o == nil {
		return base
	}
	if o.Emotion != nil {
		base.Emotion = *o.Emotion
	}
	if o.MaxFaces != nil {
		base.MaxFaces = *o.MaxFaces
	}
	if o.Padding != nil {
		base.Padding = *o.Padding
	}
	if o.MinSize != nil {
		base.Detect.MinSize = *o.MinSize
	}
	if o.MaxSize != nil {
		base.Detect.MaxSize = *o.MaxSize
	}
	if o.ShiftFactor != nil {
		base.Detect.ShiftFactor = *o.ShiftFactor
	}
	if o.ScaleFactor != nil {
		base.Detect.ScaleFactor = *o.ScaleFactor
	}
	if o.IoUThreshold != nil {
		base.Detect.IoUThreshold = *o.IoUThreshold
	}
	if o.QualityThreshold != nil {
		base.Detect.QualityThreshold = *o.QualityThreshold
	}
	return base
}

// cResponse marshals r into a C string the caller must free with fl_free.
func cResponse(r apiResponse) *C.char {
	b, err := json.Marshal(r)
	if err != nil {
		// marshalling our own types should not fail; fall back to a literal
		// so the caller still receives parseable JSON rather than nullptr
		return C.CString(`{"ok":false,"error":"facelib: could not encode response","face":false,"count":0}`)
	}
	return C.CString(string(b))
}

func cError(err error) *C.char {
	return cResponse(apiResponse{OK: false, Error: err.Error()})
}

// versionString is allocated once at init and intentionally never freed, so
// that fl_version can hand out a pointer with static lifetime.
var versionString = C.CString(version)

// fl_version returns the library version. The returned string is owned by the
// library, is valid for its lifetime, and must not be passed to fl_free.
//
//export fl_version
func fl_version() *C.char {
	return versionString
}

// fl_preload decodes the emotion model ahead of time and returns a JSON
// envelope. The returned string must be released with fl_free.
//
//export fl_preload
func fl_preload() *C.char {
	if err := facelib.PreloadEmotionModel(); err != nil {
		return cError(err)
	}
	return cResponse(apiResponse{OK: true})
}

// fl_analyze_data detects faces in a raw pixel buffer and classifies their emotion.
//
//	buf         pointer to the first pixel
//	buf_len     length of that buffer in bytes, used to bounds-check the frame
//	width       frame width in pixels
//	height      frame height in pixels
//	stride      bytes per row; pass 0 for width*channels
//	channels    1 for grayscale, 3 for RGB, 4 for RGBA
//	opts_json   optional JSON options, may be NULL or empty for defaults
//
// The buffer is only read for the duration of the call and is never retained,
// so the caller may free it as soon as fl_analyze_data returns. The returned string
// must be released with fl_free.
//
//export fl_analyze_data
func fl_analyze_data(
	buf unsafe.Pointer,
	buf_len C.int,
	width C.int,
	height C.int,
	stride C.int,
	channels C.int,
	opts_json *C.char,
) *C.char {
	if buf == nil || buf_len <= 0 {
		return cError(facelib.ErrEmptyBuffer)
	}

	opts, err := parseOptions(opts_json)
	if err != nil {
		return cError(err)
	}

	// The slice aliases caller-owned C memory. It stays within this call and
	// GrayFromBuffer copies out of it immediately, so Go never holds a
	// reference past return.
	pixels := unsafe.Slice((*byte)(buf), int(buf_len))

	res, err := facelib.AnalyzeData(
		pixels, int(width), int(height), int(stride), int(channels), opts,
	)
	if err != nil {
		return cError(err)
	}

	return cResponse(apiResponse{
		OK:    true,
		Face:  res.Face,
		Count: res.Count,
		Faces: res.Faces,
	})
}

// fl_analyze_file decodes a PNG or JPEG from disk and analyzes it.
//
//	path        NUL-terminated filesystem path
//	opts_json   optional JSON options, may be NULL or empty for defaults
//
// Decoding happens here so callers need no imaging library of their own. The
// returned string must be released with fl_free.
//
//export fl_analyze_file
func fl_analyze_file(path *C.char, opts_json *C.char) *C.char {
	if path == nil {
		return cError(errors.New("facelib: no path given"))
	}

	opts, err := parseOptions(opts_json)
	if err != nil {
		return cError(err)
	}

	res, err := facelib.AnalyzeFile(C.GoString(path), opts)
	if err != nil {
		return cError(err)
	}

	return cResponse(apiResponse{
		OK:    true,
		Face:  res.Face,
		Count: res.Count,
		Faces: res.Faces,
	})
}

// fl_free releases a string returned by this library. Passing nullptr is a no-op.
// Do not pass the result of fl_version!
//
//export fl_free
func fl_free(p *C.char) {
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}
