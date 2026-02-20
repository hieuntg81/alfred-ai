package tool

// Mu-law (G.711) audio codec with 256-entry lookup tables for fast conversion.
// Used for bidirectional audio between Twilio (8kHz mu-law) and TTS/STT (24kHz PCM).

const (
	mulawBias = 0x84
	mulawClip = 32635
)

// mulawToLinearTable is a pre-computed lookup table for mu-law to 16-bit signed PCM.
var mulawToLinearTable [256]int16

func init() {
	for i := 0; i < 256; i++ {
		mulawToLinearTable[i] = decodeMulaw(byte(i))
	}
}

// decodeMulaw converts a single mu-law byte to a 16-bit signed PCM sample.
func decodeMulaw(mulaw byte) int16 {
	mulaw = ^mulaw
	sign := int16(mulaw & 0x80)
	exponent := int(mulaw>>4) & 0x07
	mantissa := int(mulaw & 0x0F)
	sample := int16((mantissa<<3 | mulawBias) << exponent)
	sample -= int16(mulawBias << exponent) - mulawBias
	// The standard formula: sample = ((mantissa << 3) + bias) << exponent - bias
	// Simplified:
	sample = int16(((2*mantissa + 33) << uint(exponent)) - 33)
	if sign != 0 {
		sample = -sample
	}
	return sample
}

// linearToMulawTable is a pre-computed segmented lookup for PCM to mu-law.
// For speed, we use the standard algorithm directly (the table-based approach
// for full 16-bit range would need 65536 entries which is wasteful).

// LinearToMulaw converts a 16-bit signed PCM sample to a mu-law byte.
func LinearToMulaw(sample int16) byte {
	// Determine sign.
	sign := 0
	if sample < 0 {
		sign = 0x80
		sample = -sample
	}

	// Clip to range.
	if sample > mulawClip {
		sample = mulawClip
	}
	sample += mulawBias

	// Find the exponent (segment).
	exponent := 7
	expMask := int16(0x4000)
	for i := 0; i < 8; i++ {
		if sample&expMask != 0 {
			break
		}
		exponent--
		expMask >>= 1
	}

	// Extract mantissa.
	mantissa := int((sample >> uint(exponent+3)) & 0x0F)

	// Compose the mu-law byte.
	return byte(^(sign | (exponent << 4) | mantissa))
}

// MulawToLinear converts a single mu-law byte to a 16-bit signed PCM sample
// using the pre-computed lookup table.
func MulawToLinear(mulaw byte) int16 {
	return mulawToLinearTable[mulaw]
}

// MulawBufToLinear converts a buffer of mu-law bytes to 16-bit signed PCM (little-endian).
func MulawBufToLinear(mulaw []byte) []byte {
	pcm := make([]byte, len(mulaw)*2)
	for i, b := range mulaw {
		sample := mulawToLinearTable[b]
		pcm[i*2] = byte(sample)
		pcm[i*2+1] = byte(sample >> 8)
	}
	return pcm
}

// LinearBufToMulaw converts a buffer of 16-bit signed PCM (little-endian) to mu-law.
func LinearBufToMulaw(pcm []byte) []byte {
	n := len(pcm) / 2
	mulaw := make([]byte, n)
	for i := 0; i < n; i++ {
		sample := int16(pcm[i*2]) | int16(pcm[i*2+1])<<8
		mulaw[i] = LinearToMulaw(sample)
	}
	return mulaw
}

// Resample24kTo8k downsamples 16-bit PCM from 24kHz to 8kHz (3:1 decimation).
// Input: little-endian 16-bit signed PCM at 24kHz.
// Output: little-endian 16-bit signed PCM at 8kHz.
// Uses simple averaging of 3 samples for anti-aliasing.
func Resample24kTo8k(pcm24k []byte) []byte {
	samplesIn := len(pcm24k) / 2
	samplesOut := samplesIn / 3
	if samplesOut == 0 {
		return nil
	}

	out := make([]byte, samplesOut*2)
	for i := 0; i < samplesOut; i++ {
		srcIdx := i * 3
		// Average 3 samples for simple anti-aliasing.
		s0 := int32(int16(pcm24k[srcIdx*2]) | int16(pcm24k[srcIdx*2+1])<<8)
		s1 := int32(int16(pcm24k[(srcIdx+1)*2]) | int16(pcm24k[(srcIdx+1)*2+1])<<8)
		s2 := int32(int16(pcm24k[(srcIdx+2)*2]) | int16(pcm24k[(srcIdx+2)*2+1])<<8)
		avg := int16((s0 + s1 + s2) / 3)
		out[i*2] = byte(avg)
		out[i*2+1] = byte(avg >> 8)
	}
	return out
}

// Resample8kTo24k upsamples 16-bit PCM from 8kHz to 24kHz (1:3 interpolation).
// Uses linear interpolation between samples.
func Resample8kTo24k(pcm8k []byte) []byte {
	samplesIn := len(pcm8k) / 2
	if samplesIn == 0 {
		return nil
	}

	samplesOut := samplesIn * 3
	out := make([]byte, samplesOut*2)

	getSample := func(idx int) int32 {
		if idx >= samplesIn {
			idx = samplesIn - 1
		}
		return int32(int16(pcm8k[idx*2]) | int16(pcm8k[idx*2+1])<<8)
	}

	for i := 0; i < samplesIn; i++ {
		s0 := getSample(i)
		s1 := getSample(i + 1)
		outIdx := i * 3

		// First sample: exact.
		v0 := int16(s0)
		out[outIdx*2] = byte(v0)
		out[outIdx*2+1] = byte(v0 >> 8)

		// Second sample: 1/3 interpolation.
		v1 := int16((2*s0 + s1) / 3)
		out[(outIdx+1)*2] = byte(v1)
		out[(outIdx+1)*2+1] = byte(v1 >> 8)

		// Third sample: 2/3 interpolation.
		v2 := int16((s0 + 2*s1) / 3)
		out[(outIdx+2)*2] = byte(v2)
		out[(outIdx+2)*2+1] = byte(v2 >> 8)
	}
	return out
}

// RingBuffer is a bounded circular buffer for audio data with a configurable max size.
type RingBuffer struct {
	buf   []byte
	size  int
	start int
	count int
}

// NewRingBuffer creates a ring buffer with the given max capacity in bytes.
func NewRingBuffer(maxSize int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]byte, maxSize),
		size: maxSize,
	}
}

// Write appends data to the ring buffer, overwriting oldest data if full.
func (r *RingBuffer) Write(data []byte) {
	for _, b := range data {
		idx := (r.start + r.count) % r.size
		r.buf[idx] = b
		if r.count == r.size {
			// Buffer full, advance start (overwrite oldest).
			r.start = (r.start + 1) % r.size
		} else {
			r.count++
		}
	}
}

// Read reads up to n bytes from the buffer, removing them.
func (r *RingBuffer) Read(n int) []byte {
	if n > r.count {
		n = r.count
	}
	if n == 0 {
		return nil
	}

	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = r.buf[(r.start+i)%r.size]
	}
	r.start = (r.start + n) % r.size
	r.count -= n
	return out
}

// Len returns the number of bytes currently in the buffer.
func (r *RingBuffer) Len() int {
	return r.count
}

// Clear empties the buffer.
func (r *RingBuffer) Clear() {
	r.start = 0
	r.count = 0
}
