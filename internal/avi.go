package internal

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
)

// encodeMJPEGAVI writes frames as an AVI 1.0 file with one MJPG video stream:
// every frame is a JPEG of its own, so two identical pictures decode to
// identical pixels. Nothing outside the standard library is needed, so the
// tester carries no binary fixtures.
func encodeMJPEGAVI(frames []image.Image, fps int, quality int) ([]byte, error) {
	width := frames[0].Bounds().Dx()
	height := frames[0].Bounds().Dy()

	var jpegs [][]byte
	maxFrame := 0
	for _, frame := range frames {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, frame, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		jpegs = append(jpegs, buf.Bytes())
		maxFrame = max(maxFrame, buf.Len())
	}

	le := binary.LittleEndian
	u32 := func(b *bytes.Buffer, v uint32) { _ = binary.Write(b, le, v) }
	u16 := func(b *bytes.Buffer, v uint16) { _ = binary.Write(b, le, v) }
	chunk := func(b *bytes.Buffer, id string, data []byte) {
		b.WriteString(id)
		u32(b, uint32(len(data)))
		b.Write(data)
		if len(data)%2 == 1 {
			b.WriteByte(0)
		}
	}
	list := func(b *bytes.Buffer, kind string, data []byte) {
		b.WriteString("LIST")
		u32(b, uint32(4+len(data)))
		b.WriteString(kind)
		b.Write(data)
	}

	var avih bytes.Buffer
	u32(&avih, uint32(1_000_000/fps)) // dwMicroSecPerFrame
	u32(&avih, uint32(maxFrame*fps))  // dwMaxBytesPerSec
	u32(&avih, 0)                     // dwPaddingGranularity
	u32(&avih, 0x10)                  // dwFlags: AVIF_HASINDEX
	u32(&avih, uint32(len(frames)))   // dwTotalFrames
	u32(&avih, 0)                     // dwInitialFrames
	u32(&avih, 1)                     // dwStreams
	u32(&avih, uint32(maxFrame))      // dwSuggestedBufferSize
	u32(&avih, uint32(width))
	u32(&avih, uint32(height))
	avih.Write(make([]byte, 16)) // dwReserved

	var strh bytes.Buffer
	strh.WriteString("vids")
	strh.WriteString("MJPG")
	u32(&strh, 0)                   // dwFlags
	u16(&strh, 0)                   // wPriority
	u16(&strh, 0)                   // wLanguage
	u32(&strh, 0)                   // dwInitialFrames
	u32(&strh, 1)                   // dwScale
	u32(&strh, uint32(fps))         // dwRate
	u32(&strh, 0)                   // dwStart
	u32(&strh, uint32(len(frames))) // dwLength
	u32(&strh, uint32(maxFrame))    // dwSuggestedBufferSize
	u32(&strh, 0xFFFFFFFF)          // dwQuality
	u32(&strh, 0)                   // dwSampleSize
	u16(&strh, 0)                   // rcFrame
	u16(&strh, 0)
	u16(&strh, uint16(width))
	u16(&strh, uint16(height))

	var strf bytes.Buffer // BITMAPINFOHEADER
	u32(&strf, 40)
	u32(&strf, uint32(width))
	u32(&strf, uint32(height))
	u16(&strf, 1)  // biPlanes
	u16(&strf, 24) // biBitCount
	strf.WriteString("MJPG")
	u32(&strf, uint32(width*height*3))
	u32(&strf, 0)
	u32(&strf, 0)
	u32(&strf, 0)
	u32(&strf, 0)

	var strl bytes.Buffer
	chunk(&strl, "strh", strh.Bytes())
	chunk(&strl, "strf", strf.Bytes())

	var hdrl bytes.Buffer
	chunk(&hdrl, "avih", avih.Bytes())
	list(&hdrl, "strl", strl.Bytes())

	var movi, idx1 bytes.Buffer
	for _, data := range jpegs {
		offset := 4 + movi.Len() // idx1 offsets count from the 'movi' fourcc
		chunk(&movi, "00dc", data)
		idx1.WriteString("00dc")
		u32(&idx1, 0x10) // AVIIF_KEYFRAME
		u32(&idx1, uint32(offset))
		u32(&idx1, uint32(len(data)))
	}

	var body bytes.Buffer
	body.WriteString("AVI ")
	list(&body, "hdrl", hdrl.Bytes())
	list(&body, "movi", movi.Bytes())
	chunk(&body, "idx1", idx1.Bytes())

	var out bytes.Buffer
	out.WriteString("RIFF")
	u32(&out, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
