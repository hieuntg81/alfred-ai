//go:build mdns

package node

import (
	"io"
	"log/slog"
	"testing"

	"alfred-ai/internal/domain"
	"github.com/grandcat/zeroconf"
)

func mdnsTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestMDNSDiscovererCreation(t *testing.T) {
	d := NewMDNSDiscoverer(mdnsTestLogger())
	if d == nil {
		t.Fatal("expected non-nil discoverer")
	}
}

func TestEntryToNode(t *testing.T) {
	entry := zeroconf.NewServiceEntry("test-node", mdnsServiceType, mdnsDomain)
	entry.Port = 9090
	entry.Text = []string{"id=node-1", "platform=linux"}
	// Simulate an IPv4 address.
	entry.AddrIPv4 = append(entry.AddrIPv4, []byte{192, 168, 1, 10})

	node := entryToNode(entry)
	if node.ID != "node-1" {
		t.Errorf("ID = %q, want node-1", node.ID)
	}
	if node.Name != "test-node" {
		t.Errorf("Name = %q, want test-node", node.Name)
	}
	if node.Platform != "linux" {
		t.Errorf("Platform = %q, want linux", node.Platform)
	}
	if node.Address != "192.168.1.10:9090" {
		t.Errorf("Address = %q, want 192.168.1.10:9090", node.Address)
	}
	if node.Status != domain.NodeStatusOnline {
		t.Errorf("Status = %q, want online", node.Status)
	}
}

func TestParseTXTRecords(t *testing.T) {
	records := []string{"key1=val1", "key2=val2", "key3=val=with=equals"}
	m := parseTXTRecords(records)
	if m["key1"] != "val1" {
		t.Errorf("key1 = %q", m["key1"])
	}
	if m["key3"] != "val=with=equals" {
		t.Errorf("key3 = %q", m["key3"])
	}
}
