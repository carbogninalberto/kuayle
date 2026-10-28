// Package imageinfo identifies uploaded raster images by their content, never
// by a client-supplied name or type, and reads their dimensions without
// decoding pixels.
package imageinfo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"  // register GIF headers
	_ "image/jpeg" // register JPEG headers
	_ "image/png"  // register PNG headers
)

// MaxDimension bounds either side of an accepted image.
const MaxDimension = 16384

var ErrUnsupported = errors.New("unsupported image")

type Info struct {
	ContentType string
	Width       int
	Height      int
}

// Inspect accepts PNG, JPEG, GIF and WebP content with sane dimensions.
func Inspect(data []byte) (Info, error) {
	var info Info
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		info.ContentType = "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		info.ContentType = "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		info.ContentType = "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		width, height, err := webpSize(data)
		if err != nil {
			return Info{}, err
		}
		info = Info{ContentType: "image/webp", Width: width, Height: height}
	default:
		return Info{}, ErrUnsupported
	}
	if info.ContentType != "image/webp" {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return Info{}, ErrUnsupported
		}
		info.Width, info.Height = config.Width, config.Height
	}
	if info.Width <= 0 || info.Height <= 0 || info.Width > MaxDimension || info.Height > MaxDimension {
		return Info{}, ErrUnsupported
	}
	return info, nil
}

func webpSize(data []byte) (int, int, error) {
	if len(data) < 20 {
		return 0, 0, ErrUnsupported
	}
	chunk, payload := string(data[12:16]), data[20:]
	if size := binary.LittleEndian.Uint32(data[16:20]); int(size) > len(payload) {
		return 0, 0, ErrUnsupported
	}
	switch chunk {
	case "VP8X":
		if len(payload) < 10 {
			return 0, 0, ErrUnsupported
		}
		width := 1 + (int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16)
		height := 1 + (int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16)
		return width, height, nil
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			return 0, 0, ErrUnsupported
		}
		bits := binary.LittleEndian.Uint32(payload[1:5])
		return int(bits&0x3fff) + 1, int(bits>>14&0x3fff) + 1, nil
	case "VP8 ":
		if len(payload) < 10 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
			return 0, 0, ErrUnsupported
		}
		return int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff), nil
	}
	return 0, 0, ErrUnsupported
}
