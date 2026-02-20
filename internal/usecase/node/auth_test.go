package node

import (
	"errors"
	"sync"
	"testing"

	"alfred-ai/internal/domain"
)

func TestGenerateToken(t *testing.T) {
	a := NewAuth()
	token, err := a.GenerateToken("node-1")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(token) != 64 { // 32 bytes hex-encoded
		t.Errorf("token length = %d, want 64", len(token))
	}
}

func TestValidateSuccess(t *testing.T) {
	a := NewAuth()
	token, _ := a.GenerateToken("node-1")

	if err := a.ValidateToken("node-1", token); err != nil {
		t.Errorf("ValidateToken: %v", err)
	}
}

func TestValidateWrongToken(t *testing.T) {
	a := NewAuth()
	a.GenerateToken("node-1")

	err := a.ValidateToken("node-1", "wrong-token")
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
	if !errors.Is(err, domain.ErrNodeAuth) {
		t.Errorf("expected ErrNodeAuth, got: %v", err)
	}
}

func TestValidateUnknownNode(t *testing.T) {
	a := NewAuth()

	err := a.ValidateToken("unknown", "some-token")
	if err == nil {
		t.Fatal("expected error for unknown node")
	}
	if !errors.Is(err, domain.ErrNodeAuth) {
		t.Errorf("expected ErrNodeAuth, got: %v", err)
	}
}

func TestRevokeToken(t *testing.T) {
	a := NewAuth()
	token, _ := a.GenerateToken("node-1")

	a.RevokeToken("node-1")

	if err := a.ValidateToken("node-1", token); err == nil {
		t.Error("expected error after revocation")
	}
	if a.HasToken("node-1") {
		t.Error("HasToken should be false after revocation")
	}
}

func TestOverwriteToken(t *testing.T) {
	a := NewAuth()
	oldToken, _ := a.GenerateToken("node-1")
	newToken, _ := a.GenerateToken("node-1")

	if oldToken == newToken {
		t.Error("new token should differ from old token")
	}

	// Old token should no longer work.
	if err := a.ValidateToken("node-1", oldToken); err == nil {
		t.Error("old token should be invalid after overwrite")
	}
	// New token should work.
	if err := a.ValidateToken("node-1", newToken); err != nil {
		t.Errorf("new token should be valid: %v", err)
	}
}

func TestHasToken(t *testing.T) {
	a := NewAuth()
	if a.HasToken("node-1") {
		t.Error("HasToken should be false before generation")
	}

	a.GenerateToken("node-1")
	if !a.HasToken("node-1") {
		t.Error("HasToken should be true after generation")
	}
}

func TestAuthConcurrentAccess(t *testing.T) {
	a := NewAuth()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			nodeID := "node-concurrent"
			token, err := a.GenerateToken(nodeID)
			if err != nil {
				t.Errorf("GenerateToken: %v", err)
				return
			}
			_ = a.ValidateToken(nodeID, token)
			_ = a.HasToken(nodeID)
		}(i)
	}
	wg.Wait()
}
