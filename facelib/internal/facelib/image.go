package facelib

import (
	"errors"
	"image"

	xdraw "golang.org/x/image/draw"
)

// Supported channel counts for the caller-provided pixel buffer.
const (
	ChannelsGray = 1
	ChannelsRGB  = 3
	ChannelsRGBA = 4
)

var (
	ErrEmptyBuffer  = errors.New("facelib: empty pixel buffer")
	ErrBadDims      = errors.New("facelib: width and height must be positive")
	ErrBadChannels  = errors.New("facelib: channels must be 1, 3 or 4")
	ErrShortBuffer  = errors.New("facelib: pixel buffer shorter than height*stride")
	ErrBadStride    = errors.New("facelib: stride smaller than width*channels")
	ErrEmptyRegion  = errors.New("facelib: empty crop region")
	ErrBadDestScale = errors.New("facelib: destination size must be positive")
)

// Rect is an axis-aligned region in image space.
type Rect struct {
	X, Y, W, H int
}

func (r Rect) rectangle() image.Rectangle {
	return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
}

// GrayFromBuffer converts a caller-owned pixel buffer into a grayscale image.
// The buffer is never retained: the returned image owns a fresh copy, so the caller
// may free its memory as soon as this returns.
//
// stride is the number of bytes per row, which may exceed width*channels when
// rows are padded. Channel order is assumed to be R, G, B[, A].
//
// why: Go's builtin image/draw has no 24-bit RGB image type to wrap a 3-channel buffer
// with, and draw would composite the alpha channel
func GrayFromBuffer(buf []uint8, width, height, stride, channels int) (*image.Gray, error) {
	if len(buf) == 0 {
		return nil, ErrEmptyBuffer
	}
	if width <= 0 || height <= 0 {
		return nil, ErrBadDims
	}
	if channels != ChannelsGray && channels != ChannelsRGB && channels != ChannelsRGBA {
		return nil, ErrBadChannels
	}
	if stride <= 0 {
		stride = width * channels
	}
	if stride < width*channels {
		return nil, ErrBadStride
	}

	// The final row only needs width*channels bytes, not a full stride, so a
	// caller passing an exactly-sized unpadded buffer is not rejected.
	if need := (height-1)*stride + width*channels; len(buf) < need {
		return nil, ErrShortBuffer
	}

	g := image.NewGray(image.Rect(0, 0, width, height))

	if channels == ChannelsGray {
		for y := range height {
			src := buf[y*stride:]
			copy(g.Pix[y*g.Stride:y*g.Stride+width], src[:width])
		}
		return g, nil
	}

	// a pixel spans `channels` bytes, so ranging the row would step one byte at
	// a time, slices.Chunk fits but benchmarks about 40% slower in this hot loop
	for y := range height {
		row := buf[y*stride:]
		dst := g.Pix[y*g.Stride:]
		for x := range width {
			i := x * channels
			// Rec. 601 luma in fixed point (weights x 2^16, +1<<15 rounds)
			// the 0x101 widening and >>24 match color.GrayModel bit for bit
			r := uint32(row[i]) * 0x101
			gg := uint32(row[i+1]) * 0x101
			b := uint32(row[i+2]) * 0x101
			dst[x] = uint8((19595*r + 38470*gg + 7471*b + 1<<15) >> 24)
		}
	}

	return g, nil
}

// GrayFromImage converts a decoded image to grayscale, anchored at (0, 0).
//
// NRGBA is handled directly so that alpha is ignored rather than composited,
// matching GrayFromBuffer. Everything else goes through draw, which picks the
// right conversion for the concrete type, including a JPEG's YCbCr.
func GrayFromImage(src image.Image) *image.Gray {
	b := src.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))

	if n, ok := src.(*image.NRGBA); ok {
		for y := range b.Dy() {
			row := n.Pix[n.PixOffset(b.Min.X, b.Min.Y+y):]
			dst := g.Pix[y*g.Stride:]
			for x := range b.Dx() {
				i := x * 4
				r := uint32(row[i]) * 0x101
				gg := uint32(row[i+1]) * 0x101
				bb := uint32(row[i+2]) * 0x101
				dst[x] = uint8((19595*r + 38470*gg + 7471*bb + 1<<15) >> 24)
			}
		}
		return g
	}

	xdraw.Draw(g, g.Bounds(), src, b.Min, xdraw.Src)
	return g
}

// Crop returns the sub-image covered by r, clamped to the image bounds.
//
// The result shares pixels with g rather than copying them.
func Crop(g *image.Gray, r Rect) (*image.Gray, error) {
	rect := r.rectangle().Intersect(g.Bounds())
	if rect.Empty() {
		return nil, ErrEmptyRegion
	}
	return g.SubImage(rect).(*image.Gray), nil
}

// Resize resamples src to dw*dh.
//
// CatmullRom widens its kernel when minifying, so a large face crop is
// averaged down to 64x64 instead of point sampled and aliased.
func Resize(src *image.Gray, dw, dh int) (*image.Gray, error) {
	if dw <= 0 || dh <= 0 {
		return nil, ErrBadDestScale
	}
	if src.Bounds().Empty() {
		return nil, ErrEmptyRegion
	}

	dst := image.NewGray(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst, nil
}
