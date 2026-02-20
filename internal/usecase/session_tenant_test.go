package usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOrCreateWithTenant_SetsTenantID(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	s := sm.GetOrCreateWithTenant("key1", "tenant-a")
	assert.Equal(t, "tenant-a", s.TenantID)
}

func TestGetOrCreateWithTenant_ExistingSessionDifferentTenant(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	s1 := sm.GetOrCreateWithTenant("key1", "tenant-a")
	s1.TenantID = "tenant-a"

	// Same key, different tenant — should get a new session.
	s2 := sm.GetOrCreateWithTenant("key1", "tenant-b")
	assert.Equal(t, "tenant-b", s2.TenantID)
}

func TestGetWithTenant_ValidatesOwnership(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	sm.GetOrCreateWithTenant("key1", "tenant-a")

	// Same tenant — should work.
	s, err := sm.GetWithTenant("key1", "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", s.TenantID)

	// Different tenant — should fail.
	_, err = sm.GetWithTenant("key1", "tenant-b")
	assert.Error(t, err)

	// Empty tenant — backward compat, should work.
	s, err = sm.GetWithTenant("key1", "")
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", s.TenantID)
}

func TestDeleteWithTenant_ValidatesOwnership(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	sm.GetOrCreateWithTenant("key1", "tenant-a")

	// Wrong tenant — should fail.
	err := sm.DeleteWithTenant("key1", "tenant-b")
	assert.Error(t, err)

	// Right tenant — should work.
	err = sm.DeleteWithTenant("key1", "tenant-a")
	assert.NoError(t, err)
}

func TestListSessionsForTenant_FiltersCorrectly(t *testing.T) {
	sm := NewSessionManager(t.TempDir())

	sm.GetOrCreateWithTenant("key1", "tenant-a")
	sm.GetOrCreateWithTenant("key2", "tenant-b")
	sm.GetOrCreateWithTenant("key3", "tenant-a")
	sm.GetOrCreateWithTenant("key4", "") // no tenant

	// Tenant A should see its sessions + unscoped ones.
	ids := sm.ListSessionsForTenant("tenant-a")
	assert.Len(t, ids, 3) // key1, key3, key4

	// Tenant B should see its session + unscoped ones.
	ids = sm.ListSessionsForTenant("tenant-b")
	assert.Len(t, ids, 2) // key2, key4

	// Empty tenant — see all.
	ids = sm.ListSessionsForTenant("")
	assert.Len(t, ids, 4)
}
