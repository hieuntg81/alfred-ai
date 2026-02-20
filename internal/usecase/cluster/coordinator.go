// Package cluster provides distributed coordination for horizontal scaling.
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"alfred-ai/internal/domain"
)

// RedisClient abstracts the Redis operations needed by ClusterCoordinator.
// This allows a real go-redis client or a mock to be used interchangeably.
type RedisClient interface {
	// SetNX sets key to value if it does not exist. Returns true if set.
	SetNX(ctx context.Context, key string, value string, expiration time.Duration) (bool, error)
	// Del deletes one or more keys.
	Del(ctx context.Context, keys ...string) error
	// Get retrieves the value of a key.
	Get(ctx context.Context, key string) (string, error)
	// Publish publishes a message to a channel.
	Publish(ctx context.Context, channel string, message string) error
	// Subscribe subscribes to a channel. Returns a channel of messages.
	Subscribe(ctx context.Context, channel string) (<-chan string, error)
	// Close shuts down the client.
	Close() error
}

// ClusterCoordinator provides distributed session locking and event
// broadcasting for horizontal scaling via Redis.
type ClusterCoordinator struct {
	nodeID  string
	client  RedisClient
	logger  *slog.Logger
	lockTTL time.Duration

	mu       sync.Mutex
	stopCh   chan struct{}
	handlers []EventHandler
}

// EventHandler processes cluster events received from other nodes.
type EventHandler func(ctx context.Context, event domain.Event)

// CoordinatorConfig holds configuration for the cluster coordinator.
type CoordinatorConfig struct {
	NodeID  string
	LockTTL time.Duration // default: 30s
}

// NewClusterCoordinator creates a new coordinator with the given Redis client.
func NewClusterCoordinator(client RedisClient, cfg CoordinatorConfig, logger *slog.Logger) *ClusterCoordinator {
	lockTTL := cfg.LockTTL
	if lockTTL == 0 {
		lockTTL = 30 * time.Second
	}
	return &ClusterCoordinator{
		nodeID:  cfg.NodeID,
		client:  client,
		logger:  logger,
		lockTTL: lockTTL,
		stopCh:  make(chan struct{}),
	}
}

// NodeID returns this node's identifier.
func (c *ClusterCoordinator) NodeID() string { return c.nodeID }

// AcquireSession attempts to acquire a distributed lock for the given session.
// Returns true if the lock was acquired, false if another node holds it.
func (c *ClusterCoordinator) AcquireSession(ctx context.Context, sessionID string) (bool, error) {
	key := "alfred:session:lock:" + sessionID
	acquired, err := c.client.SetNX(ctx, key, c.nodeID, c.lockTTL)
	if err != nil {
		return false, fmt.Errorf("acquire session lock: %w", err)
	}
	if acquired {
		c.logger.Debug("session lock acquired", "session", sessionID, "node", c.nodeID)
	}
	return acquired, nil
}

// ReleaseSession releases the distributed lock for the given session.
// Only releases if this node holds the lock.
func (c *ClusterCoordinator) ReleaseSession(ctx context.Context, sessionID string) error {
	key := "alfred:session:lock:" + sessionID

	// Check ownership before deleting.
	owner, err := c.client.Get(ctx, key)
	if err != nil {
		// Key doesn't exist or error — either way, nothing to release.
		return nil
	}
	if owner != c.nodeID {
		c.logger.Debug("skipping lock release (not owner)",
			"session", sessionID, "owner", owner, "node", c.nodeID)
		return nil
	}

	if err := c.client.Del(ctx, key); err != nil {
		return fmt.Errorf("release session lock: %w", err)
	}
	c.logger.Debug("session lock released", "session", sessionID, "node", c.nodeID)
	return nil
}

// PublishEvent broadcasts a domain event to all cluster nodes.
func (c *ClusterCoordinator) PublishEvent(ctx context.Context, event domain.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return c.client.Publish(ctx, "alfred:events", string(data))
}

// SubscribeEvents registers a handler for cluster events and starts listening.
func (c *ClusterCoordinator) SubscribeEvents(ctx context.Context, handler EventHandler) error {
	c.mu.Lock()
	c.handlers = append(c.handlers, handler)
	c.mu.Unlock()

	ch, err := c.client.Subscribe(ctx, "alfred:events")
	if err != nil {
		return fmt.Errorf("subscribe events: %w", err)
	}

	go func() {
		for {
			select {
			case <-c.stopCh:
				return
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var event domain.Event
				if err := json.Unmarshal([]byte(msg), &event); err != nil {
					c.logger.Warn("failed to unmarshal cluster event", "error", err)
					continue
				}
				c.mu.Lock()
				handlers := append([]EventHandler{}, c.handlers...)
				c.mu.Unlock()
				for _, h := range handlers {
					h(ctx, event)
				}
			}
		}
	}()
	return nil
}

// Stop shuts down the coordinator.
func (c *ClusterCoordinator) Stop() error {
	close(c.stopCh)
	return c.client.Close()
}
