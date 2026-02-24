package seal

import (
	"container/list"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
)

// DefaultCacheSize is the default number of entries in the decryption cache.
const DefaultCacheSize = 1000

type lruEntry struct {
	key       string
	plaintext string
}

// Sealer encrypts and decrypts values using AES-256-GCM.
// Decrypted values are cached in a fixed-size LRU cache.
type Sealer struct {
	aead         cipher.AEAD
	maxCacheSize int
	mu           sync.Mutex
	items        map[string]*list.Element
	order        *list.List // front = most recent
}

// New creates a Sealer from a hex-encoded 32-byte AES key.
// maxCacheSize controls the LRU cache capacity (0 uses DefaultCacheSize).
func New(hexKey string, maxCacheSize int) (*Sealer, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("seal key must be 32 bytes (got %d)", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	if maxCacheSize <= 0 {
		maxCacheSize = DefaultCacheSize
	}

	return &Sealer{
		aead:         aead,
		maxCacheSize: maxCacheSize,
		items:        make(map[string]*list.Element),
		order:        list.New(),
	}, nil
}

// Encrypt seals a plaintext string and returns a base64-encoded ciphertext.
// Format: base64(nonce || ciphertext+tag)
func (s *Sealer) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := s.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// CacheSize returns the maximum number of entries the LRU cache can hold.
func (s *Sealer) CacheSize() int {
	return s.maxCacheSize
}

// Decrypt unseals a base64-encoded ciphertext and returns the plaintext.
// Results are cached in a fixed-size LRU cache keyed by ciphertext.
func (s *Sealer) Decrypt(sealed string) (string, error) {
	s.mu.Lock()
	if el, ok := s.items[sealed]; ok {
		s.order.MoveToFront(el)
		plaintext := el.Value.(*lruEntry).plaintext
		s.mu.Unlock()
		return plaintext, nil
	}
	s.mu.Unlock()

	// Cache miss — decrypt
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}

	nonceSize := s.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce := raw[:nonceSize]
	ciphertext := raw[nonceSize:]

	plaintext, err := s.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	// Store in cache, evict LRU if at capacity
	s.mu.Lock()
	// Double-check after acquiring write lock
	if el, ok := s.items[sealed]; ok {
		s.order.MoveToFront(el)
		s.mu.Unlock()
		return string(plaintext), nil
	}
	entry := &lruEntry{key: sealed, plaintext: string(plaintext)}
	el := s.order.PushFront(entry)
	s.items[sealed] = el
	if s.order.Len() > s.maxCacheSize {
		oldest := s.order.Back()
		s.order.Remove(oldest)
		delete(s.items, oldest.Value.(*lruEntry).key)
	}
	s.mu.Unlock()

	return string(plaintext), nil
}
