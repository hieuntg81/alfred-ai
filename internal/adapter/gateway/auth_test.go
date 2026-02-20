package gateway

import (
	"errors"
	"testing"

	"alfred-ai/internal/domain"
)

func TestStaticTokenAuthValid(t *testing.T) {
	auth := NewStaticTokenAuth([]struct {
		Token string
		Name  string
		Roles []string
	}{
		{Token: "secret-123", Name: "admin-bot", Roles: []string{"admin"}},
	})

	info, err := auth.Authenticate("secret-123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if info.Name != "admin-bot" {
		t.Errorf("Name = %q", info.Name)
	}
	if len(info.Roles) != 1 || info.Roles[0] != "admin" {
		t.Errorf("Roles = %v", info.Roles)
	}
}

func TestStaticTokenAuthInvalid(t *testing.T) {
	auth := NewStaticTokenAuth([]struct {
		Token string
		Name  string
		Roles []string
	}{
		{Token: "secret-123", Name: "admin-bot", Roles: []string{"admin"}},
	})

	_, err := auth.Authenticate("wrong-token")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrGatewayAuthFailed) {
		t.Errorf("err = %v, want ErrGatewayAuthFailed", err)
	}
}

func TestStaticTokenAuthEmpty(t *testing.T) {
	auth := NewStaticTokenAuth(nil)

	_, err := auth.Authenticate("anything")
	if err == nil {
		t.Fatal("expected error for empty token list")
	}
}
