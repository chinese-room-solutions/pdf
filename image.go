package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// ImageFormat represents the format of an extracted image.
type ImageFormat string

const (
	ImageFormatJPEG ImageFormat = "jpeg"
	ImageFormatPNG  ImageFormat = "png"
)

// ExtractedImage represents an image extracted from a PDF page.
type ExtractedImage struct {
	// Index is the 1-based sequential index of the image on the page.
	Index int
	// Data contains the image bytes (JPEG or PNG encoded).
	Data []byte
	// Format indicates the encoding of Data.
	Format ImageFormat
	// Width is the image width in pixels.
	Width int
	// Height is the image height in pixels.
	Height int
}

// extractImage reads an image XObject and returns the image data.
// It handles DCTDecode (JPEG passthrough) and raw pixel data (DeviceRGB, DeviceGray).
func extractImage(xobj Value) (*ExtractedImage, error) {
	width := int(xobj.Key("Width").Int64())
	height := int(xobj.Key("Height").Int64())
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}

	bpc := int(xobj.Key("BitsPerComponent").Int64())
	cs := xobj.Key("ColorSpace")

	// Check if the image is JPEG-encoded (DCTDecode filter).
	filter := xobj.Key("Filter")
	if isJPEG(filter) {
		data, err := readStream(xobj)
		if err != nil {
			return nil, fmt.Errorf("reading JPEG stream: %w", err)
		}
		return &ExtractedImage{
			Data:   data,
			Format: ImageFormatJPEG,
			Width:  width,
			Height: height,
		}, nil
	}

	// Raw pixel data — decode and encode as PNG.
	data, err := readStream(xobj)
	if err != nil {
		return nil, fmt.Errorf("reading image stream: %w", err)
	}

	img, err := decodeRawImage(data, width, height, bpc, cs)
	if err != nil {
		return nil, fmt.Errorf("decoding raw image: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encoding PNG: %w", err)
	}

	return &ExtractedImage{
		Data:   buf.Bytes(),
		Format: ImageFormatPNG,
		Width:  width,
		Height: height,
	}, nil
}

// isJPEG checks if the filter chain contains DCTDecode (JPEG).
func isJPEG(filter Value) bool {
	switch filter.Kind() {
	case Name:
		return filter.Name() == "DCTDecode"
	case Array:
		for i := 0; i < filter.Len(); i++ {
			if filter.Index(i).Name() == "DCTDecode" {
				return true
			}
		}
	}
	return false
}

// readStream reads and decompresses the full stream data from a Value.
// For JPEG images we need the raw bytes without PDF decompression,
// since DCTDecode IS the image format. We read raw for JPEG.
func readStream(v Value) ([]byte, error) {
	rc := v.Reader()
	defer rc.Close()
	return io.ReadAll(rc)
}

// decodeRawImage converts raw pixel data into a Go image.Image.
func decodeRawImage(data []byte, width, height, bpc int, cs Value) (image.Image, error) {
	csName := resolveColorSpace(cs)

	switch csName {
	case "DeviceRGB":
		return decodeRGB(data, width, height, bpc)
	case "DeviceGray":
		return decodeGray(data, width, height, bpc)
	case "DeviceCMYK":
		return decodeCMYK(data, width, height, bpc)
	default:
		// Fallback: try as RGB if we have enough data, otherwise gray.
		expectedRGB := width * height * 3
		if bpc == 8 && len(data) >= expectedRGB {
			return decodeRGB(data, width, height, bpc)
		}
		return decodeGray(data, width, height, bpc)
	}
}

// resolveColorSpace returns the base color space name.
func resolveColorSpace(cs Value) string {
	switch cs.Kind() {
	case Name:
		return cs.Name()
	case Array:
		// e.g. [/ICCBased stream] or [/Indexed /DeviceRGB ...]
		if cs.Len() > 0 {
			name := cs.Index(0).Name()
			if name == "ICCBased" && cs.Len() > 1 {
				// Check the N (number of components) in the ICC profile stream.
				profile := cs.Index(1)
				n := int(profile.Key("N").Int64())
				switch n {
				case 1:
					return "DeviceGray"
				case 3:
					return "DeviceRGB"
				case 4:
					return "DeviceCMYK"
				}
			}
			if name == "Indexed" {
				return "Indexed"
			}
			return name
		}
	}
	return "Unknown"
}

func decodeRGB(data []byte, width, height, bpc int) (image.Image, error) {
	if bpc != 8 {
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for RGB", bpc)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	stride := width * 3
	for y := 0; y < height; y++ {
		rowStart := y * stride
		if rowStart+stride > len(data) {
			break
		}
		for x := 0; x < width; x++ {
			i := rowStart + x*3
			img.Set(x, y, color.RGBA{R: data[i], G: data[i+1], B: data[i+2], A: 255})
		}
	}
	return img, nil
}

func decodeGray(data []byte, width, height, bpc int) (image.Image, error) {
	img := image.NewGray(image.Rect(0, 0, width, height))
	switch bpc {
	case 8:
		for y := 0; y < height; y++ {
			rowStart := y * width
			if rowStart+width > len(data) {
				break
			}
			copy(img.Pix[y*img.Stride:], data[rowStart:rowStart+width])
		}
	case 1:
		// 1-bit (black and white), packed 8 pixels per byte.
		bytesPerRow := (width + 7) / 8
		for y := 0; y < height; y++ {
			rowStart := y * bytesPerRow
			if rowStart+bytesPerRow > len(data) {
				break
			}
			for x := 0; x < width; x++ {
				byteIdx := rowStart + x/8
				bitIdx := 7 - uint(x%8)
				if data[byteIdx]&(1<<bitIdx) != 0 {
					img.SetGray(x, y, color.Gray{Y: 255})
				} else {
					img.SetGray(x, y, color.Gray{Y: 0})
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for Gray", bpc)
	}
	return img, nil
}

func decodeCMYK(data []byte, width, height, bpc int) (image.Image, error) {
	if bpc != 8 {
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for CMYK", bpc)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	stride := width * 4
	for y := 0; y < height; y++ {
		rowStart := y * stride
		if rowStart+stride > len(data) {
			break
		}
		for x := 0; x < width; x++ {
			i := rowStart + x*4
			c, m, yk, k := data[i], data[i+1], data[i+2], data[i+3]
			r, g, b := cmykToRGB(c, m, yk, k)
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img, nil
}

func cmykToRGB(c, m, y, k byte) (r, g, b byte) {
	cf := float64(c) / 255
	mf := float64(m) / 255
	yf := float64(y) / 255
	kf := float64(k) / 255
	r = byte(255 * (1 - cf) * (1 - kf))
	g = byte(255 * (1 - mf) * (1 - kf))
	b = byte(255 * (1 - yf) * (1 - kf))
	return
}
