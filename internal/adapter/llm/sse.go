package llm

import (
	"bufio"
	"bytes"
	"context"
	"io"

	"alfred-ai/internal/domain"
)

// parseSSEStream reads SSE-formatted lines from body and converts each data
// payload into a StreamDelta using the provider-specific parseLine function.
// The returned channel is closed when the stream ends, the body is closed, or
// ctx is cancelled.
func parseSSEStream(ctx context.Context, body io.ReadCloser, parseLine func(data []byte) (*domain.StreamDelta, error)) <-chan domain.StreamDelta {
	ch := make(chan domain.StreamDelta, 16)
	go func() {
		defer close(ch)
		defer body.Close()

		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Bytes()

			// Skip empty lines and comments.
			if len(line) == 0 || line[0] == ':' {
				continue
			}

			// We only care about "data: ..." lines.
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			data := bytes.TrimPrefix(line, []byte("data: "))

			// Common termination signal.
			if bytes.Equal(data, []byte("[DONE]")) {
				ch <- domain.StreamDelta{Done: true}
				return
			}

			delta, err := parseLine(data)
			if err != nil {
				// Skip unparseable lines.
				continue
			}
			if delta == nil {
				continue
			}

			select {
			case ch <- *delta:
			case <-ctx.Done():
				return
			}

			if delta.Done {
				return
			}
		}
		// If the scanner stopped due to an I/O error (not EOF), send a
		// final Done delta so consumers know the stream terminated.
		if err := scanner.Err(); err != nil {
			select {
			case ch <- domain.StreamDelta{Done: true}:
			case <-ctx.Done():
			}
		}
	}()
	return ch
}
