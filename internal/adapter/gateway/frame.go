package gateway

import "encoding/json"

// FrameType identifies the kind of frame sent over the WebSocket connection.
type FrameType string

const (
	FrameTypeRequest  FrameType = "request"
	FrameTypeResponse FrameType = "response"
	FrameTypeEvent    FrameType = "event"
)

// Frame is the envelope exchanged between client and server over WebSocket.
type Frame struct {
	Type    FrameType       `json:"type"`
	ID      uint64          `json:"id,omitempty"`      // request/response correlation ID
	Method  string          `json:"method,omitempty"`   // RPC method name (request only)
	Payload json.RawMessage `json:"payload,omitempty"`  // request params or response result
	Error   string          `json:"error,omitempty"`    // error description (response only)
}
