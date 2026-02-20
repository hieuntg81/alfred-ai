package security

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestEncryptorKeyStore_CurrentKey(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	key, err := store.CurrentKey(context.Background())
	if err != nil {
		t.Fatalf("CurrentKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}
}

func TestEncryptorKeyStore_Rotate(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	// Encrypt something with the original key.
	original := "secret data"
	encrypted, err := enc.Encrypt(original)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	oldKey, _ := store.CurrentKey(context.Background())

	// Rotate.
	_, err = store.Rotate(context.Background())
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	newKey, _ := store.CurrentKey(context.Background())

	// Keys should differ after rotation.
	if string(oldKey) == string(newKey) {
		t.Error("key did not change after rotation")
	}

	// Old encrypted data should fail to decrypt with new key.
	_, err = enc.Decrypt(encrypted)
	if err == nil {
		t.Error("expected decryption to fail after rotation")
	}
}

func TestEncryptorKeyStore_ListExpiring(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	keys, err := store.ListExpiring(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("ListExpiring: %v", err)
	}
	if keys != nil {
		t.Errorf("expected nil, got %v", keys)
	}
}

func TestKeyRotator_RotateNow(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	rotator := NewKeyRotator(store, time.Hour, testLogger())

	var callbackCalled atomic.Bool
	rotator.SetOnRotate(func(newKey []byte) {
		callbackCalled.Store(true)
		if len(newKey) == 0 {
			t.Error("callback received empty key")
		}
	})

	if err := rotator.RotateNow(context.Background()); err != nil {
		t.Fatalf("RotateNow: %v", err)
	}

	if !callbackCalled.Load() {
		t.Error("onRotate callback was not called")
	}
}

func TestKeyRotator_ScheduledRotation(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	// Very short interval for testing.
	rotator := NewKeyRotator(store, 50*time.Millisecond, testLogger())

	var rotationCount atomic.Int32
	rotator.SetOnRotate(func(_ []byte) {
		rotationCount.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	go rotator.Start(ctx)

	// Wait enough for at least 2 rotations.
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Wait for stop.
	<-rotator.done

	count := rotationCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 rotations, got %d", count)
	}
}

func TestKeyRotator_StartStop(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	rotator := NewKeyRotator(store, time.Hour, testLogger())

	ctx := context.Background()
	go rotator.Start(ctx)

	// Give it time to start.
	time.Sleep(10 * time.Millisecond)

	// Stop should not block indefinitely.
	done := make(chan struct{})
	go func() {
		rotator.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() timed out")
	}
}

func TestKeyRotator_DoubleStart(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}

	store := NewEncryptorKeyStore(enc)
	rotator := NewKeyRotator(store, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go rotator.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	// Second Start should be a no-op and not panic.
	go rotator.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	cancel()
	<-rotator.done
}
