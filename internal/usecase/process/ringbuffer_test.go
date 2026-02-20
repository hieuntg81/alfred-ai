package process

import (
	"sync"
	"testing"
)

func TestRingBuffer_BasicWriteRead(t *testing.T) {
	rb := newRingBuffer(1024)
	rb.Write([]byte("hello"))
	if got := rb.String(); got != "hello" {
		t.Errorf("String() = %q, want %q", got, "hello")
	}
	if got := rb.Len(); got != 5 {
		t.Errorf("Len() = %d, want 5", got)
	}
}

func TestRingBuffer_MultipleWrites(t *testing.T) {
	rb := newRingBuffer(1024)
	rb.Write([]byte("hello "))
	rb.Write([]byte("world"))
	if got := rb.String(); got != "hello world" {
		t.Errorf("String() = %q, want %q", got, "hello world")
	}
}

func TestRingBuffer_Overflow(t *testing.T) {
	rb := newRingBuffer(10)
	rb.Write([]byte("0123456789")) // exactly 10
	if got := rb.Len(); got != 10 {
		t.Errorf("Len() = %d, want 10", got)
	}

	rb.Write([]byte("ABCDE")) // push to 15, should trim to last 10
	if got := rb.Len(); got != 10 {
		t.Errorf("Len() after overflow = %d, want 10", got)
	}
	if got := rb.String(); got != "56789ABCDE" {
		t.Errorf("String() after overflow = %q, want %q", got, "56789ABCDE")
	}
}

func TestRingBuffer_ReadFrom(t *testing.T) {
	rb := newRingBuffer(1024)
	rb.Write([]byte("abcdefghij"))

	tests := []struct {
		offset int64
		want   string
	}{
		{0, "abcdefghij"},
		{5, "fghij"},
		{10, ""}, // at end
		{20, ""}, // past end
	}

	for _, tt := range tests {
		if got := rb.ReadFrom(tt.offset); got != tt.want {
			t.Errorf("ReadFrom(%d) = %q, want %q", tt.offset, got, tt.want)
		}
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := newRingBuffer(1024)
	if got := rb.String(); got != "" {
		t.Errorf("String() on empty = %q, want empty", got)
	}
	if got := rb.Len(); got != 0 {
		t.Errorf("Len() on empty = %d, want 0", got)
	}
	var zero int64
	if got := rb.ReadFrom(zero); got != "" {
		t.Errorf("ReadFrom(0) on empty = %q, want empty", got)
	}
}

func TestRingBuffer_ConcurrentWrites(t *testing.T) {
	rb := newRingBuffer(1024 * 1024) // 1MB
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rb.Write([]byte("x"))
			}
		}()
	}
	wg.Wait()

	if got := rb.Len(); got != 10000 {
		t.Errorf("Len() after concurrent writes = %d, want 10000", got)
	}
}

func TestRingBuffer_LargeOverflow(t *testing.T) {
	rb := newRingBuffer(5)
	// Write way more than capacity in one shot
	rb.Write([]byte("abcdefghijklmnop"))
	if got := rb.Len(); got != 5 {
		t.Errorf("Len() = %d, want 5", got)
	}
	if got := rb.String(); got != "lmnop" {
		t.Errorf("String() = %q, want %q", got, "lmnop")
	}
}

func TestRingBuffer_TotalWritten(t *testing.T) {
	rb := newRingBuffer(10)
	rb.Write([]byte("hello")) // 5 bytes
	if got := rb.TotalWritten(); got != 5 {
		t.Errorf("TotalWritten() = %d, want 5", got)
	}
	rb.Write([]byte("world!")) // 6 more bytes, total 11, buffer overflows to 10
	if got := rb.TotalWritten(); got != int64(11) {
		t.Errorf("TotalWritten() = %d, want 11", got)
	}
	if got := rb.Len(); got != 10 {
		t.Errorf("Len() = %d, want 10", got)
	}
}

func TestRingBuffer_OverflowReadFrom(t *testing.T) {
	rb := newRingBuffer(10)
	rb.Write([]byte("0123456789")) // 10 bytes, totalWritten=10

	// Read from offset 5 (should give "56789")
	if got := rb.ReadFrom(5); got != "56789" {
		t.Errorf("ReadFrom(5) before overflow = %q, want %q", got, "56789")
	}

	// Save the poll index (like Poll would)
	pollIndex := rb.TotalWritten() // 10

	// Now write more, causing overflow
	rb.Write([]byte("ABCDE")) // totalWritten=15, buffer="56789ABCDE"

	// ReadFrom with old pollIndex should still return the new data
	if got := rb.ReadFrom(pollIndex); got != "ABCDE" {
		t.Errorf("ReadFrom(%d) after overflow = %q, want %q", pollIndex, got, "ABCDE")
	}

	// ReadFrom with offset pointing to dropped data should return everything available
	var zero int64
	if got := rb.ReadFrom(zero); got != "56789ABCDE" {
		t.Errorf("ReadFrom(0) after overflow = %q, want %q", got, "56789ABCDE")
	}

	// ReadFrom at current total should return empty
	if got := rb.ReadFrom(rb.TotalWritten()); got != "" {
		t.Errorf("ReadFrom(TotalWritten) = %q, want empty", got)
	}
}

func TestRingBuffer_OverflowReadFromMultipleOverflows(t *testing.T) {
	rb := newRingBuffer(5)

	rb.Write([]byte("abcde"))      // totalWritten=5, buffer="abcde"
	pollIndex := rb.TotalWritten() // 5

	rb.Write([]byte("fghij")) // totalWritten=10, buffer="fghij" (all old data dropped)

	// Old pollIndex pointed at data that's now been fully dropped,
	// but new data starts after it, so we should get the new data.
	if got := rb.ReadFrom(pollIndex); got != "fghij" {
		t.Errorf("ReadFrom(%d) = %q, want %q", pollIndex, got, "fghij")
	}

	pollIndex = rb.TotalWritten() // 10

	rb.Write([]byte("klmno")) // totalWritten=15, buffer="klmno"
	if got := rb.ReadFrom(pollIndex); got != "klmno" {
		t.Errorf("ReadFrom(%d) = %q, want %q", pollIndex, got, "klmno")
	}
}
