//go:build grpc_node

// Package proto contains the protocol buffer message types for the node gRPC service.
//
// These types are hand-written Go structs with JSON serialization instead of
// protobuf-generated code. This avoids requiring protoc for building while
// maintaining wire compatibility via gRPC's JSON codec.
//
// To regenerate proper protobuf code from node.proto:
//   protoc --go_out=. --go-grpc_out=. node.proto
package proto

// ExecuteRequest is the request for the Execute RPC.
type ExecuteRequest struct {
	Capability string `json:"capability"`
	Params     []byte `json:"params,omitempty"`
}

// ExecuteResponse is the response from the Execute RPC.
type ExecuteResponse struct {
	Result []byte `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RegisterRequest is the request for the Register RPC.
type RegisterRequest struct {
	NodeId       string            `json:"node_id"`
	Name         string            `json:"name"`
	Platform     string            `json:"platform"`
	Token        string            `json:"token"`
	Capabilities []*Capability     `json:"capabilities,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// RegisterResponse is the response from the Register RPC.
type RegisterResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// HeartbeatRequest is the request for the Heartbeat RPC.
type HeartbeatRequest struct {
	NodeId string `json:"node_id"`
}

// HeartbeatResponse is the response from the Heartbeat RPC.
type HeartbeatResponse struct {
	Ok bool `json:"ok"`
}

// Capability describes a single node capability.
type Capability struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  []byte `json:"parameters,omitempty"`
}

// CapabilitiesResponse is the response from the ListCapabilities RPC.
type CapabilitiesResponse struct {
	Capabilities []*Capability `json:"capabilities"`
}

// Empty is an empty message.
type Empty struct{}
