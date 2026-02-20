//go:build grpc_node

// Hand-written gRPC service definitions for the node service.
// Uses a JSON codec for wire format since we don't have protoc-generated code.

package proto

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

func init() {
	// NOTE: This globally registers a JSON codec for all gRPC connections in
	// the process. Individual calls select it via grpc.CallContentSubtype("json"),
	// so protobuf-based services are unaffected unless they also explicitly
	// request the "json" content subtype. This registration is required for
	// CallContentSubtype("json") to find a matching codec.
	encoding.RegisterCodec(jsonCodec{})
}

// jsonCodec implements grpc encoding.Codec using JSON.
type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
func (jsonCodec) Name() string                      { return "json" }

// NodeServiceClient is the client API for NodeService.
type NodeServiceClient interface {
	Execute(ctx context.Context, in *ExecuteRequest, opts ...grpc.CallOption) (*ExecuteResponse, error)
	Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error)
	Heartbeat(ctx context.Context, in *HeartbeatRequest, opts ...grpc.CallOption) (*HeartbeatResponse, error)
	ListCapabilities(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*CapabilitiesResponse, error)
}

type nodeServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewNodeServiceClient creates a new NodeServiceClient.
func NewNodeServiceClient(cc grpc.ClientConnInterface) NodeServiceClient {
	return &nodeServiceClient{cc}
}

func (c *nodeServiceClient) Execute(ctx context.Context, in *ExecuteRequest, opts ...grpc.CallOption) (*ExecuteResponse, error) {
	out := new(ExecuteResponse)
	opts = append(opts, grpc.CallContentSubtype("json"))
	err := c.cc.Invoke(ctx, "/alfredai.node.v1.NodeService/Execute", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nodeServiceClient) Register(ctx context.Context, in *RegisterRequest, opts ...grpc.CallOption) (*RegisterResponse, error) {
	out := new(RegisterResponse)
	opts = append(opts, grpc.CallContentSubtype("json"))
	err := c.cc.Invoke(ctx, "/alfredai.node.v1.NodeService/Register", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nodeServiceClient) Heartbeat(ctx context.Context, in *HeartbeatRequest, opts ...grpc.CallOption) (*HeartbeatResponse, error) {
	out := new(HeartbeatResponse)
	opts = append(opts, grpc.CallContentSubtype("json"))
	err := c.cc.Invoke(ctx, "/alfredai.node.v1.NodeService/Heartbeat", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *nodeServiceClient) ListCapabilities(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*CapabilitiesResponse, error) {
	out := new(CapabilitiesResponse)
	opts = append(opts, grpc.CallContentSubtype("json"))
	err := c.cc.Invoke(ctx, "/alfredai.node.v1.NodeService/ListCapabilities", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// NodeServiceServer is the server API for NodeService.
type NodeServiceServer interface {
	Execute(context.Context, *ExecuteRequest) (*ExecuteResponse, error)
	Register(context.Context, *RegisterRequest) (*RegisterResponse, error)
	Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error)
	ListCapabilities(context.Context, *Empty) (*CapabilitiesResponse, error)
	mustEmbedUnimplementedNodeServiceServer()
}

// UnimplementedNodeServiceServer provides default implementations.
type UnimplementedNodeServiceServer struct{}

func (UnimplementedNodeServiceServer) Execute(context.Context, *ExecuteRequest) (*ExecuteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Execute not implemented")
}
func (UnimplementedNodeServiceServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Register not implemented")
}
func (UnimplementedNodeServiceServer) Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Heartbeat not implemented")
}
func (UnimplementedNodeServiceServer) ListCapabilities(context.Context, *Empty) (*CapabilitiesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListCapabilities not implemented")
}
func (UnimplementedNodeServiceServer) mustEmbedUnimplementedNodeServiceServer() {}

// UnsafeNodeServiceServer may be embedded to opt out of forward compatibility.
type UnsafeNodeServiceServer interface {
	mustEmbedUnimplementedNodeServiceServer()
}

// RegisterNodeServiceServer registers the NodeService with a gRPC server.
func RegisterNodeServiceServer(s grpc.ServiceRegistrar, srv NodeServiceServer) {
	s.RegisterService(&NodeService_ServiceDesc, srv)
}

func _NodeService_Execute_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ExecuteRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeServiceServer).Execute(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/alfredai.node.v1.NodeService/Execute"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeServiceServer).Execute(ctx, req.(*ExecuteRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NodeService_Register_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RegisterRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeServiceServer).Register(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/alfredai.node.v1.NodeService/Register"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeServiceServer).Register(ctx, req.(*RegisterRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NodeService_Heartbeat_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HeartbeatRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeServiceServer).Heartbeat(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/alfredai.node.v1.NodeService/Heartbeat"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeServiceServer).Heartbeat(ctx, req.(*HeartbeatRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _NodeService_ListCapabilities_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(NodeServiceServer).ListCapabilities(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/alfredai.node.v1.NodeService/ListCapabilities"}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(NodeServiceServer).ListCapabilities(ctx, req.(*Empty))
	}
	return interceptor(ctx, in, info, handler)
}

// NodeService_ServiceDesc is the grpc.ServiceDesc for NodeService.
var NodeService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "alfredai.node.v1.NodeService",
	HandlerType: (*NodeServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Execute", Handler: _NodeService_Execute_Handler},
		{MethodName: "Register", Handler: _NodeService_Register_Handler},
		{MethodName: "Heartbeat", Handler: _NodeService_Heartbeat_Handler},
		{MethodName: "ListCapabilities", Handler: _NodeService_ListCapabilities_Handler},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "node.proto",
}
