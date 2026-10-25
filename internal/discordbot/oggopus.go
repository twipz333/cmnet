package discordbot

import (
	"bytes"
	"fmt"
	"io"

	"github.com/jfreymuth/oggvorbis"
	"github.com/thesyncim/gopus"
)

const (
	discordRate      = 48000
	discordChannels  = 2 // Discord voice path is stereo
	discordFrameMs   = 20
	discordFrameSamp = discordRate * discordFrameMs / 1000 // 960
)

// OpusPacketsFromOgg extracts Discord-ready Opus packets from NTTS audio.
// NTTS ext=ogg is Ogg Vorbis (not Opus); we decode and re-encode. Real
// Opus-in-Ogg is also accepted.
func OpusPacketsFromOgg(data []byte) ([][]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("audio: empty")
	}
	if isWAV(data) {
		pcm, rate, ch, err := decodeWAV(data)
		if err != nil {
			return nil, err
		}
		return encodeDiscordOpus(pcm, rate, ch)
	}
	kind, err := sniffOggCodec(data)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "opus":
		pkts, err := extractOpusPackets(data)
		if err != nil {
			return nil, err
		}
		return pkts, nil
	case "vorbis":
		pcm, rate, ch, err := decodeVorbis(data)
		if err != nil {
			return nil, err
		}
		return encodeDiscordOpus(pcm, rate, ch)
	default:
		return nil, fmt.Errorf("ogg: unsupported codec %q (need vorbis or opus)", kind)
	}
}

func sniffOggCodec(data []byte) (string, error) {
	r := bytes.NewReader(data)
	var capture [4]byte
	if _, err := io.ReadFull(r, capture[:]); err != nil {
		return "", err
	}
	if string(capture[:]) != "OggS" {
		return "", fmt.Errorf("ogg: bad capture")
	}
	var hdr [23]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", err
	}
	nseg := int(hdr[22])
	segs := make([]byte, nseg)
	if _, err := io.ReadFull(r, segs); err != nil {
		return "", err
	}
	size := 0
	for _, n := range segs {
		size += int(n)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return "", err
	}
	if len(body) >= 8 && string(body[:8]) == "OpusHead" {
		return "opus", nil
	}
	if len(body) >= 7 && string(body[1:7]) == "vorbis" {
		return "vorbis", nil
	}
	if bytes.Contains(body, []byte("vorbis")) {
		return "vorbis", nil
	}
	return "", fmt.Errorf("ogg: unknown bos packet")
}

func extractOpusPackets(data []byte) ([][]byte, error) {
	r := bytes.NewReader(data)
	var packets [][]byte
	var opusSeen bool
	for {
		var capture [4]byte
		if _, err := io.ReadFull(r, capture[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		if string(capture[:]) != "OggS" {
			return nil, fmt.Errorf("ogg: bad page capture")
		}
		var hdr [23]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, err
		}
		nseg := int(hdr[22])
		segs := make([]byte, nseg)
		if _, err := io.ReadFull(r, segs); err != nil {
			return nil, err
		}
		var page []byte
		for _, n := range segs {
			chunk := make([]byte, int(n))
			if _, err := io.ReadFull(r, chunk); err != nil {
				return nil, err
			}
			page = append(page, chunk...)
		}
		var pkt []byte
		off := 0
		for _, n := range segs {
			pkt = append(pkt, page[off:off+int(n)]...)
			off += int(n)
			if n < 255 {
				if len(pkt) > 0 {
					if isOpusHead(pkt) || isOpusTags(pkt) {
						opusSeen = true
					} else if opusSeen {
						packets = append(packets, append([]byte(nil), pkt...))
					}
				}
				pkt = nil
			}
		}
		if len(pkt) > 0 && opusSeen && !isOpusHead(pkt) && !isOpusTags(pkt) {
			packets = append(packets, append([]byte(nil), pkt...))
		}
	}
	if !opusSeen {
		return nil, fmt.Errorf("ogg: no Opus stream")
	}
	if len(packets) == 0 {
		return nil, fmt.Errorf("ogg: no opus packets")
	}
	return packets, nil
}

func decodeVorbis(data []byte) (pcm []float32, rate, ch int, err error) {
	r, err := oggvorbis.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("vorbis: %w", err)
	}
	rate = r.SampleRate()
	ch = r.Channels()
	if rate <= 0 || ch <= 0 {
		return nil, 0, 0, fmt.Errorf("vorbis: bad format rate=%d ch=%d", rate, ch)
	}
	buf := make([]float32, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			pcm = append(pcm, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, fmt.Errorf("vorbis read: %w", err)
		}
	}
	if len(pcm) == 0 {
		return nil, 0, 0, fmt.Errorf("vorbis: empty pcm")
	}
	return pcm, rate, ch, nil
}

func encodeDiscordOpus(pcm []float32, srcRate, srcCh int) ([][]byte, error) {
	stereo := toStereo48k(pcm, srcRate, srcCh)
	enc, err := gopus.NewEncoder(gopus.EncoderConfig{
		SampleRate:  discordRate,
		Channels:    discordChannels,
		Application: gopus.ApplicationVoIP,
	})
	if err != nil {
		return nil, err
	}
	frame := discordFrameSamp * discordChannels
	outBuf := make([]byte, 4000)
	var packets [][]byte
	for off := 0; off < len(stereo); off += frame {
		chunk := stereo[off:]
		if len(chunk) < frame {
			pad := make([]float32, frame)
			copy(pad, chunk)
			chunk = pad
		} else {
			chunk = chunk[:frame]
		}
		n, err := enc.Encode(chunk, outBuf)
		if err != nil {
			return nil, fmt.Errorf("opus encode: %w", err)
		}
		if n <= 0 {
			continue // lookahead fill
		}
		packets = append(packets, append([]byte(nil), outBuf[:n]...))
	}
	if len(packets) == 0 {
		return nil, fmt.Errorf("opus: no packets produced")
	}
	return packets, nil
}

// toStereo48k resamples interleaved float PCM to 48 kHz stereo.
func toStereo48k(pcm []float32, srcRate, srcCh int) []float32 {
	if srcCh < 1 {
		srcCh = 1
	}
	frames := len(pcm) / srcCh
	if frames <= 0 {
		return nil
	}
	mono := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float32
		for c := 0; c < srcCh; c++ {
			sum += pcm[i*srcCh+c]
		}
		mono[i] = sum / float32(srcCh)
	}
	outFrames := frames
	if srcRate != discordRate {
		outFrames = int(float64(frames) * float64(discordRate) / float64(srcRate))
		if outFrames < 1 {
			outFrames = 1
		}
	}
	out := make([]float32, outFrames*discordChannels)
	if srcRate == discordRate {
		for i := 0; i < outFrames; i++ {
			s := mono[i]
			out[i*2] = s
			out[i*2+1] = s
		}
		return out
	}
	for i := 0; i < outFrames; i++ {
		srcPos := float64(i) * float64(srcRate) / float64(discordRate)
		i0 := int(srcPos)
		if i0 >= frames-1 {
			s := mono[frames-1]
			out[i*2] = s
			out[i*2+1] = s
			continue
		}
		frac := float32(srcPos - float64(i0))
		s := mono[i0]*(1-frac) + mono[i0+1]*frac
		out[i*2] = s
		out[i*2+1] = s
	}
	return out
}

func isOpusHead(p []byte) bool {
	return len(p) >= 8 && string(p[:8]) == "OpusHead"
}

func isOpusTags(p []byte) bool {
	return len(p) >= 8 && string(p[:8]) == "OpusTags"
}

func isWAV(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

func decodeWAV(data []byte) (pcm []float32, rate, ch int, err error) {
	if !isWAV(data) {
		return nil, 0, 0, fmt.Errorf("wav: not RIFF/WAVE")
	}
	// Minimal PCM wav reader (16-bit or float32).
	off := 12
	var audioFmt uint16
	var bits uint16
	var raw []byte
	for off+8 <= len(data) {
		id := string(data[off : off+4])
		sz := int(uint32(data[off+4]) | uint32(data[off+5])<<8 | uint32(data[off+6])<<16 | uint32(data[off+7])<<24)
		off += 8
		if off+sz > len(data) {
			break
		}
		chunk := data[off : off+sz]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, 0, fmt.Errorf("wav: short fmt")
			}
			audioFmt = uint16(chunk[0]) | uint16(chunk[1])<<8
			ch = int(uint16(chunk[2]) | uint16(chunk[3])<<8)
			rate = int(uint32(chunk[4]) | uint32(chunk[5])<<8 | uint32(chunk[6])<<16 | uint32(chunk[7])<<24)
			bits = uint16(chunk[14]) | uint16(chunk[15])<<8
		case "data":
			raw = chunk
		}
		off += sz
		if sz%2 == 1 {
			off++
		}
	}
	if raw == nil || ch == 0 || rate == 0 {
		return nil, 0, 0, fmt.Errorf("wav: missing fmt/data")
	}
	switch {
	case audioFmt == 1 && bits == 16:
		n := len(raw) / 2
		pcm = make([]float32, n)
		for i := 0; i < n; i++ {
			v := int16(uint16(raw[i*2]) | uint16(raw[i*2+1])<<8)
			pcm[i] = float32(v) / 32768
		}
	case audioFmt == 3 && bits == 32:
		n := len(raw) / 4
		pcm = make([]float32, n)
		for i := 0; i < n; i++ {
			u := uint32(raw[i*4]) | uint32(raw[i*4+1])<<8 | uint32(raw[i*4+2])<<16 | uint32(raw[i*4+3])<<24
			pcm[i] = float32FromBits(u)
		}
	default:
		return nil, 0, 0, fmt.Errorf("wav: unsupported format=%d bits=%d", audioFmt, bits)
	}
	return pcm, rate, ch, nil
}

func float32FromBits(u uint32) float32 {
	return float32FromBitsImpl(u)
}
