package seal

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
)

func validHexKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(key)
}

func TestEncryptDecrypt(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"Bearer ghp_realtoken123",
		"key_12345",
		"",
		"a",
		"hello world with spaces and special chars: !@#$%^&*()",
	}

	for _, plain := range tests {
		sealed, err := s.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}

		got, err := s.Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", plain, err)
		}

		if got != plain {
			t.Errorf("roundtrip failed: got %q, want %q", got, plain)
		}
	}
}

func TestEncryptProducesDifferentCiphertexts(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	a, _ := s.Encrypt("same")
	b, _ := s.Encrypt("same")

	if a == b {
		t.Error("encrypting the same plaintext should produce different ciphertexts (random nonce)")
	}
}

func TestInvalidKeyLength(t *testing.T) {
	// Too short (16 bytes)
	short := hex.EncodeToString(make([]byte, 16))
	if _, err := New(short, 0); err == nil {
		t.Error("expected error for 16-byte key")
	}

	// Too long (64 bytes)
	long := hex.EncodeToString(make([]byte, 64))
	if _, err := New(long, 0); err == nil {
		t.Error("expected error for 64-byte key")
	}

	// Not hex
	if _, err := New("not-hex-at-all!", 0); err == nil {
		t.Error("expected error for non-hex key")
	}
}

func TestTamperedCiphertext(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := s.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}

	// Decode, flip a byte, re-encode
	raw, _ := base64.StdEncoding.DecodeString(sealed)
	raw[len(raw)-1] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(raw)

	if _, err := s.Decrypt(tampered); err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Decrypt("not-valid-base64!!!"); err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecryptTooShort(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	short := base64.StdEncoding.EncodeToString([]byte("abc"))
	if _, err := s.Decrypt(short); err == nil {
		t.Error("expected error for ciphertext shorter than nonce")
	}
}

func TestDecryptCache(t *testing.T) {
	s, err := New(validHexKey(t), 100)
	if err != nil {
		t.Fatal(err)
	}

	sealed, _ := s.Encrypt("cached-value")

	// First call — cache miss, performs decryption
	got1, err := s.Decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}

	// Verify entry is in cache
	s.mu.Lock()
	_, cached := s.items[sealed]
	s.mu.Unlock()
	if !cached {
		t.Error("expected value to be cached after first Decrypt")
	}

	// Second call — should return from cache
	got2, err := s.Decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}

	if got1 != got2 || got1 != "cached-value" {
		t.Errorf("cache returned wrong value: %q / %q", got1, got2)
	}
}

func TestCacheEviction(t *testing.T) {
	s, err := New(validHexKey(t), 3)
	if err != nil {
		t.Fatal(err)
	}

	// Fill cache with 3 entries
	sealed := make([]string, 4)
	for i := 0; i < 3; i++ {
		sealed[i], _ = s.Encrypt(fmt.Sprintf("value-%d", i))
		s.Decrypt(sealed[i])
	}

	// Cache should have exactly 3 entries
	s.mu.Lock()
	if len(s.items) != 3 {
		t.Fatalf("expected 3 cached entries, got %d", len(s.items))
	}
	s.mu.Unlock()

	// Add a 4th entry — should evict the oldest (sealed[0])
	sealed[3], _ = s.Encrypt("value-3")
	s.Decrypt(sealed[3])

	s.mu.Lock()
	if len(s.items) != 3 {
		t.Fatalf("expected 3 cached entries after eviction, got %d", len(s.items))
	}
	_, hasFirst := s.items[sealed[0]]
	_, hasLast := s.items[sealed[3]]
	s.mu.Unlock()

	if hasFirst {
		t.Error("oldest entry should have been evicted")
	}
	if !hasLast {
		t.Error("newest entry should be in cache")
	}

	// Evicted entry should still decrypt correctly (just not cached)
	got, err := s.Decrypt(sealed[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "value-0" {
		t.Errorf("expected 'value-0', got %q", got)
	}
}

func TestCacheLRUOrdering(t *testing.T) {
	s, err := New(validHexKey(t), 3)
	if err != nil {
		t.Fatal(err)
	}

	// Fill cache: [0, 1, 2] (2 is most recent)
	sealed := make([]string, 4)
	for i := 0; i < 3; i++ {
		sealed[i], _ = s.Encrypt(fmt.Sprintf("value-%d", i))
		s.Decrypt(sealed[i])
	}

	// Access sealed[0] to make it most recent: order becomes [1, 2, 0]
	s.Decrypt(sealed[0])

	// Add a 4th entry — should evict sealed[1] (now the LRU)
	sealed[3], _ = s.Encrypt("value-3")
	s.Decrypt(sealed[3])

	s.mu.Lock()
	_, hasZero := s.items[sealed[0]]
	_, hasOne := s.items[sealed[1]]
	_, hasThree := s.items[sealed[3]]
	s.mu.Unlock()

	if !hasZero {
		t.Error("sealed[0] was recently accessed, should not be evicted")
	}
	if hasOne {
		t.Error("sealed[1] was LRU, should be evicted")
	}
	if !hasThree {
		t.Error("sealed[3] is newest, should be in cache")
	}
}

func TestCacheDefaultSize(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.maxCacheSize != DefaultCacheSize {
		t.Errorf("expected default cache size %d, got %d", DefaultCacheSize, s.maxCacheSize)
	}
}

func TestCacheSizeCustom(t *testing.T) {
	s, err := New(validHexKey(t), 42)
	if err != nil {
		t.Fatal(err)
	}
	if s.CacheSize() != 42 {
		t.Errorf("expected cache size 42, got %d", s.CacheSize())
	}
}

func TestNegativeCacheSize(t *testing.T) {
	s, err := New(validHexKey(t), -5)
	if err != nil {
		t.Fatal(err)
	}
	if s.maxCacheSize != DefaultCacheSize {
		t.Errorf("negative cache size should default to %d, got %d", DefaultCacheSize, s.maxCacheSize)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	s1, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := s1.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}

	// Decrypting with a different key should fail
	if _, err := s2.Decrypt(sealed); err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestEncryptEmpty(t *testing.T) {
	s, err := New(validHexKey(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := s.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}

	got, err := s.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestConcurrentDecrypt(t *testing.T) {
	s, err := New(validHexKey(t), 100)
	if err != nil {
		t.Fatal(err)
	}

	sealed, _ := s.Encrypt("concurrent")

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.Decrypt(sealed)
			if err != nil {
				errs <- err
				return
			}
			if got != "concurrent" {
				errs <- fmt.Errorf("got %q, want 'concurrent'", got)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}
