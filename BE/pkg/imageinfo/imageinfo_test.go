package imageinfo

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func sample(t *testing.T, encode func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 7, 3))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, encode(&buf, img))
	return buf.Bytes()
}

func TestInspectAcceptsRasterImages(t *testing.T) {
	for name, encode := range map[string]func(*bytes.Buffer, image.Image) error{
		"image/png":  func(b *bytes.Buffer, i image.Image) error { return png.Encode(b, i) },
		"image/jpeg": func(b *bytes.Buffer, i image.Image) error { return jpeg.Encode(b, i, nil) },
		"image/gif":  func(b *bytes.Buffer, i image.Image) error { return gif.Encode(b, i, nil) },
	} {
		info, err := Inspect(sample(t, encode))
		require.NoError(t, err, name)
		require.Equal(t, Info{ContentType: name, Width: 7, Height: 3}, info)
	}
}

func webp(chunk string, payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(4+8+len(payload)))
	buf.WriteString("WEBP")
	buf.WriteString(chunk)
	binary.Write(&buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	return buf.Bytes()
}

func TestInspectReadsWebPDimensions(t *testing.T) {
	// VP8X: flags(4) then 24-bit width-1 and height-1.
	extended := webp("VP8X", []byte{0, 0, 0, 0, 0xff, 0x01, 0, 49, 0, 0})
	info, err := Inspect(extended)
	require.NoError(t, err)
	require.Equal(t, Info{ContentType: "image/webp", Width: 512, Height: 50}, info)
	// VP8L: signature 0x2f then 14-bit width-1 and height-1.
	bits := uint32(19) | uint32(9)<<14
	lossless := webp("VP8L", []byte{0x2f, byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)})
	info, err = Inspect(lossless)
	require.NoError(t, err)
	require.Equal(t, Info{ContentType: "image/webp", Width: 20, Height: 10}, info)
	// VP8: frame tag(3), start code, 14-bit width and height.
	lossy := webp("VP8 ", []byte{0, 0, 0, 0x9d, 0x01, 0x2a, 64, 0, 32, 0})
	info, err = Inspect(lossy)
	require.NoError(t, err)
	require.Equal(t, Info{ContentType: "image/webp", Width: 64, Height: 32}, info)
}

func TestInspectRejectsOtherContent(t *testing.T) {
	for name, data := range map[string][]byte{
		"html":      []byte("<!doctype html><script>alert(1)</script>"),
		"svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`),
		"truncated": sample(t, func(b *bytes.Buffer, i image.Image) error { return png.Encode(b, i) })[:20],
		"empty":     nil,
		"pdf":       []byte("%PDF-1.4\n"),
	} {
		_, err := Inspect(data)
		require.ErrorIs(t, err, ErrUnsupported, name)
	}
}

func TestInspectRejectsHugeDimensions(t *testing.T) {
	_, err := Inspect(webp("VP8X", []byte{0, 0, 0, 0, 0xff, 0xff, 0xff, 0, 0, 0}))
	require.ErrorIs(t, err, ErrUnsupported)
}
