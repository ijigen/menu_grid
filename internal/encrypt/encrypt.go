package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

const (
	SaltSize   = 16
	KeySize    = 32 // AES-256
	NonceSize  = 12 // GCM standard
	Iterations = 100000
)

// Encryptor caches the derived key so we don't re-derive on every image request.
type Encryptor struct {
	mu       sync.RWMutex
	password string
	salt     []byte
	key      []byte
}

func NewEncryptor() *Encryptor {
	return &Encryptor{}
}

// GenerateSalt creates a new random salt and returns it base64-encoded.
func GenerateSalt() (string, error) {
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}
	return base64.StdEncoding.EncodeToString(salt), nil
}

// SetCredentials updates the password and salt, and re-derives the key.
func (e *Encryptor) SetCredentials(password, saltBase64 string) error {
	salt, err := base64.StdEncoding.DecodeString(saltBase64)
	if err != nil {
		return fmt.Errorf("invalid salt: %w", err)
	}

	key := pbkdf2.Key([]byte(password), salt, Iterations, KeySize, sha256.New)

	e.mu.Lock()
	defer e.mu.Unlock()
	e.password = password
	e.salt = salt
	e.key = key
	return nil
}

// Encrypt encrypts data with AES-256-GCM.
// Returns: [12 bytes nonce][ciphertext + GCM tag]
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	e.mu.RLock()
	key := e.key
	e.mu.RUnlock()

	if key == nil {
		return nil, fmt.Errorf("encryptor not initialized")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Prepend nonce
	result := make([]byte, NonceSize+len(ciphertext))
	copy(result[:NonceSize], nonce)
	copy(result[NonceSize:], ciphertext)
	return result, nil
}

// EncryptReader reads all data from r, encrypts, and writes to w.
func (e *Encryptor) EncryptToWriter(w io.Writer, plaintext []byte) error {
	encrypted, err := e.Encrypt(plaintext)
	if err != nil {
		return err
	}
	_, err = w.Write(encrypted)
	return err
}
