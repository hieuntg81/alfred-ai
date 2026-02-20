package nodesdk

import "log/slog"

// Option configures a NodeAgent.
type Option func(*NodeAgent)

// WithServer sets the alfred-ai server address for registration.
func WithServer(addr string) Option {
	return func(n *NodeAgent) { n.serverAddr = addr }
}

// WithToken sets the device token for authentication.
func WithToken(token string) Option {
	return func(n *NodeAgent) { n.deviceToken = token }
}

// WithPlatform sets the platform identifier (e.g., "linux/arm64").
func WithPlatform(platform string) Option {
	return func(n *NodeAgent) { n.platform = platform }
}

// WithLogger sets a custom slog.Logger.
func WithLogger(logger *slog.Logger) Option {
	return func(n *NodeAgent) { n.logger = logger }
}

// WithListenPort sets the port for incoming gRPC connections.
func WithListenPort(port int) Option {
	return func(n *NodeAgent) { n.listenPort = port }
}
