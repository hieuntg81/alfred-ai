package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"alfred-ai/internal/domain"
)

// RPCHandler handles a single RPC method call.
type RPCHandler func(ctx context.Context, client *ClientInfo, payload json.RawMessage) (json.RawMessage, error)

// clientConn tracks a single WebSocket connection.
type clientConn struct {
	info      *ClientInfo
	ws        *websocket.Conn
	sendCh    chan Frame // buffered outbound queue
	done      chan struct{}
	closeOnce sync.Once
}

// Server is the WebSocket gateway that exposes RPC methods and forwards events.
type Server struct {
	bus        domain.EventBus
	clients    sync.Map // connID (uint64) -> *clientConn
	auth       Authenticator
	handlersMu sync.RWMutex
	handlers   map[string]RPCHandler
	logger     *slog.Logger
	addr       string
	httpSrv    *http.Server
	boundAddr  string
	nextID     atomic.Uint64
	unsubAll   func()
	httpRoutes []httpRoute // additional HTTP routes
}

type httpRoute struct {
	pattern string
	handler http.HandlerFunc
}

// NewServer creates a gateway server.
func NewServer(bus domain.EventBus, auth Authenticator, addr string, logger *slog.Logger) *Server {
	return &Server{
		bus:      bus,
		auth:     auth,
		handlers: make(map[string]RPCHandler),
		logger:   logger,
		addr:     addr,
	}
}

// RegisterHandler adds an RPC handler for the given method name.
// Safe to call concurrently with active connections.
func (s *Server) RegisterHandler(method string, handler RPCHandler) {
	s.handlersMu.Lock()
	s.handlers[method] = handler
	s.handlersMu.Unlock()
}

// RegisterHTTPRoute adds an HTTP handler to the gateway's mux.
// Must be called before Start().
func (s *Server) RegisterHTTPRoute(pattern string, handler http.HandlerFunc) {
	s.httpRoutes = append(s.httpRoutes, httpRoute{pattern: pattern, handler: handler})
}

// Start begins accepting WebSocket connections. Blocks until context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleUpgrade)
	for _, route := range s.httpRoutes {
		mux.HandleFunc(route.pattern, route.handler)
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("gateway listen: %w", err)
	}
	s.boundAddr = listener.Addr().String()

	s.httpSrv = &http.Server{Handler: mux}

	// Subscribe to all events and forward to connected clients.
	unsub := s.bus.SubscribeAll(func(_ context.Context, event domain.Event) {
		payload, err := json.Marshal(event)
		if err != nil {
			return
		}
		frame := Frame{
			Type:    FrameTypeEvent,
			Payload: payload,
		}
		s.clients.Range(func(_, value any) bool {
			cc := value.(*clientConn)
			select {
			case cc.sendCh <- frame:
			default:
				s.logger.Warn("gateway: dropped event for slow client")
			}
			return true
		})
	})
	s.unsubAll = unsub

	s.logger.Info("gateway started", "addr", s.boundAddr)

	go func() {
		<-ctx.Done()
		s.Stop(context.Background())
	}()

	if err := s.httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("gateway serve: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the gateway server.
func (s *Server) Stop(ctx context.Context) error {
	if s.unsubAll != nil {
		s.unsubAll()
	}

	// Close all client connections.
	s.clients.Range(func(key, value any) bool {
		cc := value.(*clientConn)
		cc.closeOnce.Do(func() { close(cc.done) })
		cc.ws.Close(websocket.StatusGoingAway, "server shutting down")
		s.clients.Delete(key)
		return true
	})

	if s.httpSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return s.httpSrv.Shutdown(shutdownCtx)
	}
	return nil
}

// BoundAddr returns the actual address the server bound to. Only valid after Start.
func (s *Server) BoundAddr() string { return s.boundAddr }

func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	// Authenticate via query param.
	token := r.URL.Query().Get("token")
	clientInfo, err := s.auth.Authenticate(token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract optional tenant ID for multi-tenant routing.
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		clientInfo.TenantID = tenantID
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Secure origin checking: allow localhost for dev, same-origin, or explicit allowed origins
		OriginPatterns: []string{
			"localhost",
			"localhost:*",
			"127.0.0.1",
			"127.0.0.1:*",
			"[::1]",
			"[::1]:*",
		},
	})
	if err != nil {
		s.logger.Warn("websocket accept failed", "error", err)
		return
	}

	connID := s.nextID.Add(1)
	cc := &clientConn{
		info:   clientInfo,
		ws:     ws,
		sendCh: make(chan Frame, 64),
		done:   make(chan struct{}),
	}
	s.clients.Store(connID, cc)

	s.logger.Info("gateway client connected", "conn_id", connID, "client", clientInfo.Name)

	// Start write loop.
	go s.writeLoop(cc)

	// Read loop (blocking).
	s.readLoop(r.Context(), cc)

	// Cleanup.
	cc.closeOnce.Do(func() { close(cc.done) })
	s.clients.Delete(connID)
	ws.Close(websocket.StatusNormalClosure, "")
	s.logger.Info("gateway client disconnected", "conn_id", connID)
}

func (s *Server) readLoop(ctx context.Context, cc *clientConn) {
	for {
		select {
		case <-cc.done:
			return
		default:
		}

		var frame Frame
		err := wsjson.Read(ctx, cc.ws, &frame)
		if err != nil {
			return // connection closed or error
		}

		if frame.Type != FrameTypeRequest {
			continue
		}

		go s.dispatchRPC(ctx, cc, frame)
	}
}

func (s *Server) writeLoop(cc *clientConn) {
	for {
		select {
		case <-cc.done:
			return
		case frame := <-cc.sendCh:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := wsjson.Write(ctx, cc.ws, frame)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) dispatchRPC(ctx context.Context, cc *clientConn, req Frame) {
	s.handlersMu.RLock()
	handler, ok := s.handlers[req.Method]
	s.handlersMu.RUnlock()
	if !ok {
		s.sendResponse(cc, req.ID, nil, domain.ErrRPCMethodNotFound)
		return
	}

	// Inject tenant and roles into context for downstream use.
	if cc.info.TenantID != "" {
		ctx = domain.ContextWithTenantID(ctx, cc.info.TenantID)
	}
	if len(cc.info.Roles) > 0 {
		ctx = domain.ContextWithRoles(ctx, domain.StringsToAuthRoles(cc.info.Roles))
	}

	result, err := handler(ctx, cc.info, req.Payload)
	s.sendResponse(cc, req.ID, result, err)
}

func (s *Server) sendResponse(cc *clientConn, id uint64, result json.RawMessage, err error) {
	resp := Frame{
		Type:    FrameTypeResponse,
		ID:      id,
		Payload: result,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	select {
	case cc.sendCh <- resp:
	default:
		s.logger.Warn("gateway: dropped RPC response for slow client", "frame_id", id)
	}
}
