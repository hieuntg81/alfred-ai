//go:build mdns

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"

	"alfred-ai/internal/domain"
)

const (
	mdnsServiceType = "_alfredai._tcp"
	mdnsDomain      = "local."
	mdnsScanTimeout = 5 * time.Second
)

// MDNSDiscoverer discovers alfred-ai nodes on the local network via mDNS/DNS-SD.
type MDNSDiscoverer struct {
	logger *slog.Logger
}

// NewMDNSDiscoverer creates a new MDNSDiscoverer.
func NewMDNSDiscoverer(logger *slog.Logger) *MDNSDiscoverer {
	return &MDNSDiscoverer{logger: logger}
}

// Scan browses for alfred-ai services on the local network.
func (d *MDNSDiscoverer) Scan(ctx context.Context) ([]domain.Node, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("mdns resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	var mu sync.Mutex
	var nodes []domain.Node
	var wg sync.WaitGroup

	scanCtx, cancel := context.WithTimeout(ctx, mdnsScanTimeout)
	defer cancel()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			node := entryToNode(entry)
			mu.Lock()
			nodes = append(nodes, node)
			mu.Unlock()
			d.logger.Debug("mdns discovered node", "id", node.ID, "address", node.Address)
		}
	}()

	if err := resolver.Browse(scanCtx, mdnsServiceType, mdnsDomain, entries); err != nil {
		cancel()
		// Wait for consumer goroutine to drain the channel before returning.
		wg.Wait()
		return nil, fmt.Errorf("mdns browse: %w", err)
	}

	// Wait for the scan timeout to complete, then wait for the consumer
	// goroutine to finish processing all entries.
	<-scanCtx.Done()
	wg.Wait()

	mu.Lock()
	result := make([]domain.Node, len(nodes))
	copy(result, nodes)
	mu.Unlock()

	return result, nil
}

// Advertise registers this instance as a alfred-ai node on the local network.
// This method blocks until ctx is cancelled. Call it in a goroutine.
func (d *MDNSDiscoverer) Advertise(ctx context.Context, name string, port int, metadata map[string]string) error {
	txt := make([]string, 0, len(metadata))
	for k, v := range metadata {
		txt = append(txt, k+"="+v)
	}

	server, err := zeroconf.Register(name, mdnsServiceType, mdnsDomain, port, txt, nil)
	if err != nil {
		return fmt.Errorf("mdns register: %w", err)
	}

	d.logger.Info("mdns advertising", "name", name, "port", port)
	<-ctx.Done()
	server.Shutdown()
	return nil
}

func entryToNode(entry *zeroconf.ServiceEntry) domain.Node {
	var address string
	if len(entry.AddrIPv4) > 0 {
		address = fmt.Sprintf("%s:%d", entry.AddrIPv4[0], entry.Port)
	} else if len(entry.AddrIPv6) > 0 {
		address = fmt.Sprintf("[%s]:%d", entry.AddrIPv6[0], entry.Port)
	}

	metadata := parseTXTRecords(entry.Text)

	return domain.Node{
		ID:           metadata["id"],
		Name:         entry.ServiceRecord.Instance,
		Platform:     metadata["platform"],
		Address:      address,
		Status:       domain.NodeStatusOnline,
		LastSeen:     time.Now(),
		Metadata:     metadata,
		Capabilities: parseMDNSCapabilities(metadata["capabilities"]),
	}
}

func parseTXTRecords(txt []string) map[string]string {
	m := make(map[string]string, len(txt))
	for _, t := range txt {
		parts := strings.SplitN(t, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func parseMDNSCapabilities(raw string) []domain.NodeCapability {
	if raw == "" {
		return nil
	}
	var caps []domain.NodeCapability
	_ = json.Unmarshal([]byte(raw), &caps)
	return caps
}
