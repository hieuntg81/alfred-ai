package gateway

import (
	"context"
	"encoding/json"

	"alfred-ai/internal/domain"
)

// --- node handlers ---

func nodeListHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		nodes, err := deps.NodeManager.List(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(nodes)
	}
}

type nodeGetRequest struct {
	ID string `json:"id"`
}

func nodeGetHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req nodeGetRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.ID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		node, err := deps.NodeManager.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(node)
	}
}

type nodeInvokeRequest struct {
	NodeID     string          `json:"node_id"`
	Capability string          `json:"capability"`
	Params     json.RawMessage `json:"params,omitempty"`
}

func nodeInvokeHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req nodeInvokeRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.NodeID == "" || req.Capability == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		result, err := deps.NodeManager.Invoke(ctx, req.NodeID, req.Capability, req.Params)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

func nodeDiscoverHandler(deps HandlerDeps) RPCHandler {
	return func(ctx context.Context, _ *ClientInfo, _ json.RawMessage) (json.RawMessage, error) {
		nodes, err := deps.NodeManager.Discover(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(nodes)
	}
}

type nodeTokenRequest struct {
	NodeID string `json:"node_id"`
}

func nodeTokenGenerateHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req nodeTokenRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.NodeID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		token, err := deps.NodeTokens.GenerateToken(req.NodeID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"token": token})
	}
}

func nodeTokenRevokeHandler(deps HandlerDeps) RPCHandler {
	return func(_ context.Context, _ *ClientInfo, payload json.RawMessage) (json.RawMessage, error) {
		var req nodeTokenRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, domain.ErrRPCInvalidPayload
		}
		if req.NodeID == "" {
			return nil, domain.ErrRPCInvalidPayload
		}
		deps.NodeTokens.RevokeToken(req.NodeID)
		return json.Marshal(map[string]bool{"ok": true})
	}
}
