package process

import (
	"sync"
)

// ringBuffer is a thread-safe, bounded byte buffer that drops old data
// when the capacity is exceeded. Used for capturing process output.
type ringBuffer struct {
	mu      sync.Mutex
	data    []byte
	max     int
	written int64 // total bytes ever written (including dropped)
}

func newRingBuffer(maxBytes int) *ringBuffer {
	return &ringBuffer{
		data: make([]byte, 0, min(maxBytes, 4096)),
		max:  maxBytes,
	}
}

// Write implements io.Writer. Thread-safe.
func (rb *ringBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data = append(rb.data, p...)
	rb.written += int64(len(p))
	if len(rb.data) > rb.max {
		rb.data = rb.data[len(rb.data)-rb.max:]
	}
	return len(p), nil
}

// String returns the full buffered content.
func (rb *ringBuffer) String() string {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return string(rb.data)
}

// Len returns the current buffer length in bytes.
func (rb *ringBuffer) Len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return len(rb.data)
}

// TotalWritten returns the total number of bytes ever written,
// including bytes that have been dropped due to overflow.
func (rb *ringBuffer) TotalWritten() int64 {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return rb.written
}

// ReadFrom returns content from the given byte offset onward.
// The offset is in terms of total bytes written (not current buffer position).
// If data has been dropped and offset points to dropped data, reading starts
// from the beginning of the current buffer.
func (rb *ringBuffer) ReadFrom(offset int64) string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	dropped := rb.written - int64(len(rb.data))
	localOffset := offset - dropped
	if localOffset < 0 {
		localOffset = 0
	}
	if localOffset >= int64(len(rb.data)) {
		return ""
	}
	return string(rb.data[localOffset:])
}
