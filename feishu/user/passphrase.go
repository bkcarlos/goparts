package user

import (
	"crypto/sha256"
	"errors"
	"golang.org/x/crypto/pbkdf2"
)

// NewPassphraseFileStore derives an AES-256 key with PBKDF2-HMAC-SHA256. Salt must
// be randomly generated once (at least 16 bytes), stored separately and reused;
// neither salt nor passphrase is automatically persisted. Prefer secret-manager
// random keys for services. Parameters form part of the file's key configuration.
func NewPassphraseFileStore(path string, passphrase, salt []byte, iterations int) (*EncryptedFileStore, error) {
	if len(passphrase) < 12 || len(salt) < 16 || iterations < 600000 || iterations > 10000000 {
		return nil, errors.New("user: passphrase/salt/iteration policy not met")
	}
	key := pbkdf2.Key(passphrase, salt, iterations, 32, sha256.New)
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	return NewEncryptedFileStore(path, key)
}
