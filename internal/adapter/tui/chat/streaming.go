package chat

import "time"

// StreamSpeed controls how fast responses are progressively rendered.
type StreamSpeed int

const (
	StreamInstant StreamSpeed = iota // show everything immediately
	StreamFast                       // 32 runes per tick
	StreamNormal                     // 8 runes per tick (default)
)

// String returns a human-readable label for the speed.
func (s StreamSpeed) String() string {
	switch s {
	case StreamInstant:
		return "instant"
	case StreamFast:
		return "fast"
	case StreamNormal:
		return "normal"
	default:
		return "unknown"
	}
}

// StreamConfig holds streaming parameters.
type StreamConfig struct {
	Speed     StreamSpeed
	ChunkSize int           // runes per tick (0 means instant)
	TickRate  time.Duration // delay between ticks
}

// DefaultStreamConfig returns the default (normal) streaming config.
func DefaultStreamConfig() StreamConfig {
	return StreamConfigForSpeed(StreamNormal)
}

// StreamConfigForSpeed returns a config for the given speed preset.
func StreamConfigForSpeed(s StreamSpeed) StreamConfig {
	switch s {
	case StreamInstant:
		return StreamConfig{Speed: StreamInstant, ChunkSize: 0, TickRate: 0}
	case StreamFast:
		return StreamConfig{Speed: StreamFast, ChunkSize: 32, TickRate: 16 * time.Millisecond}
	default: // StreamNormal
		return StreamConfig{Speed: StreamNormal, ChunkSize: 8, TickRate: 16 * time.Millisecond}
	}
}

// CycleStreamSpeed cycles: normal → fast → instant → normal.
func CycleStreamSpeed(current StreamSpeed) StreamSpeed {
	switch current {
	case StreamNormal:
		return StreamFast
	case StreamFast:
		return StreamInstant
	case StreamInstant:
		return StreamNormal
	default:
		return StreamNormal
	}
}
