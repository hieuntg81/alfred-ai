package security

import (
	"encoding/base64"
	"sync"
	"testing"
)

func TestAESEncryptDecryptRoundTrip(t *testing.T) {
	enc, err := NewAESContentEncryptor("test-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	plaintext := "Hello, this is a secret message."
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if ciphertext == plaintext {
		t.Error("ciphertext should differ from plaintext")
	}
	if !enc.IsEncrypted(ciphertext) {
		t.Error("IsEncrypted should return true for encrypted text")
	}

	decrypted, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("Decrypt = %q, want %q", decrypted, plaintext)
	}
}

func TestAESDifferentCiphertextPerCall(t *testing.T) {
	enc, err := NewAESContentEncryptor("passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	c1, _ := enc.Encrypt("same input")
	c2, _ := enc.Encrypt("same input")

	if c1 == c2 {
		t.Error("two encryptions of same plaintext should produce different ciphertext")
	}
}

func TestAESPlaintextPassthrough(t *testing.T) {
	enc, err := NewAESContentEncryptor("passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	plain := "this is not encrypted"
	if enc.IsEncrypted(plain) {
		t.Error("plain text should not be detected as encrypted")
	}

	result, err := enc.Decrypt(plain)
	if err != nil {
		t.Fatalf("Decrypt plaintext: %v", err)
	}
	if result != plain {
		t.Errorf("plaintext passthrough: got %q, want %q", result, plain)
	}
}

func TestAESWrongKeyFails(t *testing.T) {
	enc1, _ := NewAESContentEncryptor("key-one")
	enc2, _ := NewAESContentEncryptor("key-two")
	defer enc1.Zeroize()
	defer enc2.Zeroize()

	ciphertext, _ := enc1.Encrypt("secret")
	_, err := enc2.Decrypt(ciphertext)
	if err == nil {
		t.Error("decrypting with wrong key should fail")
	}
}

func TestAESMalformedInput(t *testing.T) {
	enc, _ := NewAESContentEncryptor("key")
	defer enc.Zeroize()

	// Invalid base64 after prefix
	_, err := enc.Decrypt("enc:not-valid-base64!!!")
	if err == nil {
		t.Error("malformed input should fail")
	}

	// Valid base64 but too short for nonce
	_, err = enc.Decrypt("enc:AAAA")
	if err == nil {
		t.Error("too-short ciphertext should fail")
	}
}

func TestAESEmptyPassphraseRejected(t *testing.T) {
	_, err := NewAESContentEncryptor("")
	if err == nil {
		t.Error("empty passphrase should be rejected")
	}
}

func TestAESRotate(t *testing.T) {
	enc, _ := NewAESContentEncryptor("old-key")
	defer enc.Zeroize()

	ciphertext, _ := enc.Encrypt("before rotation")
	decrypted, _ := enc.Decrypt(ciphertext)
	if decrypted != "before rotation" {
		t.Fatalf("pre-rotate decrypt failed")
	}

	if err := enc.Rotate("new-key"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// Old ciphertext cannot be decrypted with new key
	_, err := enc.Decrypt(ciphertext)
	if err == nil {
		t.Error("old ciphertext should not decrypt after rotation")
	}

	// New encryption works
	newCT, err := enc.Encrypt("after rotation")
	if err != nil {
		t.Fatalf("Encrypt after rotate: %v", err)
	}
	newPT, err := enc.Decrypt(newCT)
	if err != nil {
		t.Fatalf("Decrypt after rotate: %v", err)
	}
	if newPT != "after rotation" {
		t.Errorf("post-rotate: got %q, want %q", newPT, "after rotation")
	}
}

func TestAESRotateEmptyPassphrase(t *testing.T) {
	enc, _ := NewAESContentEncryptor("key")
	defer enc.Zeroize()

	if err := enc.Rotate(""); err == nil {
		t.Error("Rotate with empty passphrase should fail")
	}
}

func TestAESZeroize(t *testing.T) {
	enc, _ := NewAESContentEncryptor("key")

	// Encrypt before zeroize
	ciphertext, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Verify it decrypts before zeroize
	pt, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt before zeroize: %v", err)
	}
	if pt != "secret" {
		t.Fatalf("unexpected plaintext: %q", pt)
	}

	// Zeroize clears the key
	enc.Zeroize()

	// After zeroize, decrypt should fail (key is all zeros)
	_, err = enc.Decrypt(ciphertext)
	if err == nil {
		t.Error("decrypt after zeroize should fail")
	}
}

func TestAESConcurrentEncrypt(t *testing.T) {
	enc, _ := NewAESContentEncryptor("concurrent-key")
	defer enc.Zeroize()

	var wg sync.WaitGroup
	const n = 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ct, err := enc.Encrypt("concurrent plaintext")
			if err != nil {
				t.Errorf("concurrent Encrypt: %v", err)
				return
			}
			pt, err := enc.Decrypt(ct)
			if err != nil {
				t.Errorf("concurrent Decrypt: %v", err)
				return
			}
			if pt != "concurrent plaintext" {
				t.Errorf("concurrent round-trip: got %q", pt)
			}
		}()
	}
	wg.Wait()
}

func TestDecryptCiphertextTooShort(t *testing.T) {
	enc, err := NewAESContentEncryptor("passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	// Create a valid enc: prefix but with too-short data
	shortData := "enc:" + base64.StdEncoding.EncodeToString([]byte("short"))
	_, err = enc.Decrypt(shortData)
	if err == nil {
		t.Error("expected error for ciphertext too short")
	}
}

func TestDecryptPlaintextPassthrough(t *testing.T) {
	enc, err := NewAESContentEncryptor("passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	result, err := enc.Decrypt("plaintext content")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if result != "plaintext content" {
		t.Errorf("expected passthrough, got %q", result)
	}
}

func TestRotateEmptyPassphrase(t *testing.T) {
	enc, err := NewAESContentEncryptor("original-pass")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	err = enc.Rotate("")
	if err == nil {
		t.Error("expected error for empty passphrase")
	}
}

func TestNewAESContentEncryptorEmptyPassphrase(t *testing.T) {
	_, err := NewAESContentEncryptor("")
	if err == nil {
		t.Error("expected error for empty passphrase")
	}
}

func TestEncryptWithInvalidKeyLength(t *testing.T) {
	// Create a valid encryptor then corrupt the key length to trigger aes.NewCipher error
	enc, err := NewAESContentEncryptor("valid-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	// Set key to an invalid length (not 16, 24, or 32 bytes)
	enc.mu.Lock()
	enc.key = []byte("bad") // 3 bytes - invalid for AES
	enc.mu.Unlock()

	_, err = enc.Encrypt("plaintext")
	if err == nil {
		t.Error("expected error from Encrypt with invalid key length")
	}
}

func TestDecryptWithInvalidKeyLength(t *testing.T) {
	// First encrypt with a valid key
	enc, err := NewAESContentEncryptor("valid-passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	ciphertext, err := enc.Encrypt("secret data")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Corrupt the key to an invalid length
	enc.mu.Lock()
	enc.key = []byte("bad") // 3 bytes - invalid for AES
	enc.mu.Unlock()

	_, err = enc.Decrypt(ciphertext)
	if err == nil {
		t.Error("expected error from Decrypt with invalid key length")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	enc, err := NewAESContentEncryptor("passphrase")
	if err != nil {
		t.Fatalf("NewAESContentEncryptor: %v", err)
	}
	defer enc.Zeroize()

	ciphertext, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Tamper with the base64-encoded data (flip a byte in the ciphertext portion)
	// This should cause gcm.Open to fail with an authentication error
	raw, err := base64.StdEncoding.DecodeString(ciphertext[len(encPrefix):])
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	// Flip the last byte of the sealed data
	raw[len(raw)-1] ^= 0xff
	tampered := encPrefix + base64.StdEncoding.EncodeToString(raw)

	_, err = enc.Decrypt(tampered)
	if err == nil {
		t.Error("expected error from Decrypt with tampered ciphertext")
	}
}
