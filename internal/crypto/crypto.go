// Package crypto provides encryption support for SesameFS encrypted libraries.
// It implements a dual-mode system:
//   - Compat mode: PBKDF2-SHA256 (for Seafile desktop/mobile client compatibility)
//   - Strong mode: Argon2id (for web/API clients with modern security)
//
// This mirrors the SHA-1→SHA-256 translation we do for block storage:
// Seafile clients use weak encryption, but we add server-side protection.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// Encryption versions
const (
	// EncVersionSeafileV2 is the standard Seafile encryption (weak PBKDF2)
	EncVersionSeafileV2 = 2
	// EncVersionSeafileV4 is Seafile with per-repo salt
	EncVersionSeafileV4 = 4
	// EncVersionSesameFS is our strong Argon2id encryption (web/API only)
	EncVersionSesameFS = 10
	// EncVersionDual is dual-mode: stores both weak (compat) and strong (security)
	EncVersionDual = 12
)

// Seafile v2 uses a static 8-byte salt (NOT repo_id-derived)
// This is the hardcoded salt from seafile-crypt.c
var seafileStaticSalt = []byte{0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}

// deriveSeafileSalt returns the salt for Seafile encryption
// For v2: static 8-byte salt (weak but required for compatibility)
// For v4+: per-repo random salt
func deriveSeafileSalt(repoID string, version int, salt []byte) []byte {
	if version >= EncVersionSeafileV4 && len(salt) > 0 {
		return salt
	}
	// Seafile v2 uses static salt
	return seafileStaticSalt
}

// Argon2id parameters (OWASP recommended for 2024+)
const (
	argon2Time    = 3     // iterations
	argon2Memory  = 65536 // 64 MB
	argon2Threads = 4     // parallelism
	argon2KeyLen  = 32    // 256-bit key
)

// PBKDF2 parameters
const (
	pbkdf2Iterations = 1000 // Seafile v2 uses 1000 (weak but required for compat)
	pbkdf2KeyLen     = 32   // 256-bit key
	pbkdf2IVIter     = 10   // iterations for IV derivation
)

// Salt and key sizes
const (
	SaltSize    = 32 // 256-bit random salt per library
	FileKeySize = 32 // 256-bit file encryption key
	IVSize      = 16 // 128-bit IV for AES-CBC
)

// EncryptionParams holds the encryption metadata for a library
type EncryptionParams struct {
	EncVersion int    `json:"enc_version"`
	Salt       string `json:"salt"`        // Hex-encoded 32-byte random salt
	Magic      string `json:"magic"`       // PBKDF2-derived (Seafile compat)
	MagicStrong string `json:"magic_strong,omitempty"` // Argon2id-derived (SesameFS)
	RandomKey  string `json:"random_key"`  // Encrypted file key (PBKDF2)
	RandomKeyStrong string `json:"random_key_strong,omitempty"` // Encrypted file key (Argon2id)
}

// GenerateSalt creates a cryptographically random 32-byte salt
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}

// GenerateFileKey creates a cryptographically random 32-byte file encryption key
func GenerateFileKey() ([]byte, error) {
	key := make([]byte, FileKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate file key: %w", err)
	}
	return key, nil
}

// DeriveKeyPBKDF2 derives a key using PBKDF2-HMAC-SHA256 (Seafile v2 compatible)
// This is weak but required for Seafile desktop/mobile client compatibility.
func DeriveKeyPBKDF2(password string, repoID string, salt []byte, version int) (key []byte, iv []byte) {
	// Seafile uses repo_id + password as input (from seafile-crypt.c: seafile_generate_magic)
	input := []byte(repoID + password)

	// Get the appropriate salt for this version
	deriveSalt := deriveSeafileSalt(repoID, version, salt)

	// Derive 48 bytes (32-byte key + 16-byte IV) in a single PBKDF2 call
	// This matches Seafile's implementation exactly - key and IV are derived together
	derived := pbkdf2.Key(input, deriveSalt, pbkdf2Iterations, pbkdf2KeyLen+IVSize, sha256.New)
	key = derived[:pbkdf2KeyLen]
	iv = derived[pbkdf2KeyLen:]

	return key, iv
}

// DeriveKeyArgon2id derives a key using Argon2id (strong, memory-hard)
// This is our preferred method for web/API clients.
func DeriveKeyArgon2id(password string, repoID string, salt []byte) (key []byte, iv []byte) {
	// Use repo_id + password as input (same as PBKDF2 for consistency)
	input := []byte(repoID + password)

	// Derive 48 bytes: 32 for key, 16 for IV
	derived := argon2.IDKey(input, salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen+IVSize)

	key = derived[:argon2KeyLen]
	iv = derived[argon2KeyLen:]

	return key, iv
}

// ComputeMagic computes the magic token for password verification.
// The magic token is HMAC-SHA256(derived_key, repo_id) for better security.
func ComputeMagic(repoID string, derivedKey []byte) string {
	h := hmac.New(sha256.New, derivedKey)
	h.Write([]byte(repoID))
	return hex.EncodeToString(h.Sum(nil))
}

// ComputeMagicSeafile computes the Seafile-compatible magic token.
// Seafile just uses the derived key directly as the magic (not ideal, but compatible).
func ComputeMagicSeafile(password string, repoID string, salt []byte, version int) string {
	key, _ := DeriveKeyPBKDF2(password, repoID, salt, version)
	return hex.EncodeToString(key)
}

// VerifyPassword checks if a password is correct by comparing magic tokens.
// Uses constant-time comparison to prevent timing attacks.
func VerifyPassword(computedMagic, storedMagic string) bool {
	return subtle.ConstantTimeCompare([]byte(computedMagic), []byte(storedMagic)) == 1
}

// EncryptFileKey encrypts the file key with the derived key using AES-256-CBC.
// Returns hex-encoded ciphertext.
func EncryptFileKey(fileKey []byte, derivedKey []byte, iv []byte) (string, error) {
	if len(fileKey) != FileKeySize {
		return "", errors.New("file key must be 32 bytes")
	}
	if len(derivedKey) < 32 {
		return "", errors.New("derived key must be at least 32 bytes")
	}

	block, err := aes.NewCipher(derivedKey[:32])
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// PKCS7 pad to block size
	padded := pkcs7Pad(fileKey, aes.BlockSize)

	// Encrypt with CBC
	encrypted := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv[:IVSize])
	mode.CryptBlocks(encrypted, padded)

	return hex.EncodeToString(encrypted), nil
}

// DecryptFileKey decrypts the file key using AES-256-CBC.
// Takes hex-encoded ciphertext, returns raw file key.
func DecryptFileKey(encryptedHex string, derivedKey []byte, iv []byte) ([]byte, error) {
	encrypted, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}

	if len(derivedKey) < 32 {
		return nil, errors.New("derived key must be at least 32 bytes")
	}

	block, err := aes.NewCipher(derivedKey[:32])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	if len(encrypted) < aes.BlockSize {
		return nil, errors.New("ciphertext too short")
	}

	// Decrypt with CBC
	decrypted := make([]byte, len(encrypted))
	mode := cipher.NewCBCDecrypter(block, iv[:IVSize])
	mode.CryptBlocks(decrypted, encrypted)

	// Remove PKCS7 padding
	fileKey, err := pkcs7Unpad(decrypted)
	if err != nil {
		return nil, fmt.Errorf("invalid padding: %w", err)
	}

	return fileKey, nil
}

// CreateEncryptedLibrary generates all encryption parameters for a new encrypted library.
// Returns both Seafile-compatible (weak) and SesameFS (strong) parameters.
func CreateEncryptedLibrary(password string, repoID string) (*EncryptionParams, error) {
	// Generate random salt for Argon2id (strong mode)
	randomSalt, err := GenerateSalt()
	if err != nil {
		return nil, err
	}

	// Generate random file key
	fileKey, err := GenerateFileKey()
	if err != nil {
		return nil, err
	}

	// For Seafile v2 compatibility: use repo_id-derived salt (NOT random)
	// This is critical - Seafile client derives salt from repo_id and verifies magic
	keyPBKDF2, ivPBKDF2 := DeriveKeyPBKDF2(password, repoID, nil, EncVersionSeafileV2)

	// For strong mode: use random salt with Argon2id
	keyArgon2, ivArgon2 := DeriveKeyArgon2id(password, repoID, randomSalt)

	// Compute magic tokens
	// Seafile magic uses v2 algorithm (repo_id-derived salt)
	magicSeafile := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
	magicStrong := ComputeMagic(repoID, keyArgon2)

	// Encrypt file key with both derived keys
	randomKey, err := EncryptFileKey(fileKey, keyPBKDF2, ivPBKDF2)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt file key (PBKDF2): %w", err)
	}

	randomKeyStrong, err := EncryptFileKey(fileKey, keyArgon2, ivArgon2)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt file key (Argon2id): %w", err)
	}

	return &EncryptionParams{
		EncVersion:      EncVersionDual,
		Salt:            hex.EncodeToString(randomSalt), // Store random salt for Argon2id
		Magic:           magicSeafile,                   // Seafile-compatible (repo_id-derived salt)
		MagicStrong:     magicStrong,
		RandomKey:       randomKey,
		RandomKeyStrong: randomKeyStrong,
	}, nil
}

// VerifyPasswordSeafile verifies a password using Seafile-compatible PBKDF2.
// Use this for Seafile desktop/mobile clients.
// NOTE: For dual-mode (v12) libraries, the Seafile magic is computed with repo_id-derived salt,
// so we pass nil salt and EncVersionSeafileV2 to use the correct algorithm.
func VerifyPasswordSeafile(password, repoID, storedMagic string, salt []byte, version int) bool {
	// For dual-mode libraries, Seafile magic uses repo_id-derived salt (v2 algorithm)
	if version == EncVersionDual {
		computed := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
		return VerifyPassword(computed, storedMagic)
	}
	computed := ComputeMagicSeafile(password, repoID, salt, version)
	return VerifyPassword(computed, storedMagic)
}

// VerifyPasswordStrong verifies a password using Argon2id.
// Use this for web/API clients.
func VerifyPasswordStrong(password, repoID, storedMagicStrong string, salt []byte) bool {
	keyArgon2, _ := DeriveKeyArgon2id(password, repoID, salt)
	computed := ComputeMagic(repoID, keyArgon2)
	return VerifyPassword(computed, storedMagicStrong)
}

// ChangePassword re-encrypts the file key with a new password.
// Returns updated encryption parameters.
func ChangePassword(oldPassword, newPassword, repoID string, params *EncryptionParams) (*EncryptionParams, error) {
	randomSalt, err := hex.DecodeString(params.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid salt: %w", err)
	}

	// Verify old password first (VerifyPasswordSeafile handles dual-mode automatically)
	if !VerifyPasswordSeafile(oldPassword, repoID, params.Magic, randomSalt, params.EncVersion) {
		return nil, errors.New("wrong password")
	}

	// Decrypt file key using old password
	// For dual-mode (v12), RandomKey was encrypted with repo_id-derived salt (v2 algorithm)
	var oldKey, oldIV []byte
	if params.EncVersion == EncVersionDual {
		oldKey, oldIV = DeriveKeyPBKDF2(oldPassword, repoID, nil, EncVersionSeafileV2)
	} else {
		oldKey, oldIV = DeriveKeyPBKDF2(oldPassword, repoID, randomSalt, params.EncVersion)
	}
	fileKey, err := DecryptFileKey(params.RandomKey, oldKey, oldIV)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt file key: %w", err)
	}

	// Generate new random salt for Argon2id (strong mode)
	newRandomSalt, err := GenerateSalt()
	if err != nil {
		return nil, err
	}

	// For Seafile v2 compatibility: use repo_id-derived salt (NOT random)
	// This ensures Seafile desktop client can verify the password
	newKeyPBKDF2, newIVPBKDF2 := DeriveKeyPBKDF2(newPassword, repoID, nil, EncVersionSeafileV2)

	// For strong mode: use random salt with Argon2id
	newKeyArgon2, newIVArgon2 := DeriveKeyArgon2id(newPassword, repoID, newRandomSalt)

	// Compute new magic tokens
	newMagicSeafile := ComputeMagicSeafile(newPassword, repoID, nil, EncVersionSeafileV2)
	newMagicStrong := ComputeMagic(repoID, newKeyArgon2)

	// Re-encrypt file key with new keys
	newRandomKey, err := EncryptFileKey(fileKey, newKeyPBKDF2, newIVPBKDF2)
	if err != nil {
		return nil, fmt.Errorf("failed to re-encrypt file key (PBKDF2): %w", err)
	}

	newRandomKeyStrong, err := EncryptFileKey(fileKey, newKeyArgon2, newIVArgon2)
	if err != nil {
		return nil, fmt.Errorf("failed to re-encrypt file key (Argon2id): %w", err)
	}

	return &EncryptionParams{
		EncVersion:      EncVersionDual,
		Salt:            hex.EncodeToString(newRandomSalt), // Random salt for Argon2id
		Magic:           newMagicSeafile,                   // Uses repo_id-derived salt
		MagicStrong:     newMagicStrong,
		RandomKey:       newRandomKey,                      // Uses repo_id-derived salt
		RandomKeyStrong: newRandomKeyStrong,
	}, nil
}

// pkcs7Pad pads data to a multiple of blockSize using PKCS#7
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padBytes := make([]byte, padding)
	for i := range padBytes {
		padBytes[i] = byte(padding)
	}
	return append(data, padBytes...)
}

// pkcs7Unpad removes PKCS#7 padding
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	padding := int(data[len(data)-1])
	if padding > len(data) || padding > aes.BlockSize {
		return nil, errors.New("invalid padding size")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil, errors.New("invalid padding bytes")
		}
	}
	return data[:len(data)-padding], nil
}

// EncryptBlock encrypts a block of file content using AES-256-CBC.
// Uses Seafile's block encryption format: random IV prepended to ciphertext.
// The IV is 16 bytes, generated randomly for each block.
func EncryptBlock(plaintext []byte, fileKey []byte) ([]byte, error) {
	if len(fileKey) != FileKeySize {
		return nil, errors.New("file key must be 32 bytes")
	}

	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Generate random IV for this block
	iv := make([]byte, IVSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// PKCS7 pad the plaintext
	padded := pkcs7Pad(plaintext, aes.BlockSize)

	// Encrypt with CBC
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	// Prepend IV to ciphertext (Seafile format)
	result := make([]byte, IVSize+len(ciphertext))
	copy(result[:IVSize], iv)
	copy(result[IVSize:], ciphertext)

	return result, nil
}

// DecryptBlock decrypts a block of file content using AES-256-CBC.
// Expects Seafile format: 16-byte IV prepended to ciphertext.
// If the block doesn't appear to be encrypted (wrong size), it returns the data as-is.
// This handles legacy blocks that were stored before encryption was implemented.
func DecryptBlock(encrypted []byte, fileKey []byte) ([]byte, error) {
	if len(fileKey) != FileKeySize {
		return nil, errors.New("file key must be 32 bytes")
	}

	// Check if block appears to be encrypted:
	// - Must be at least IV (16) + one AES block (16) = 32 bytes
	// - Ciphertext (len - 16) must be a multiple of 16 (AES block size)
	if len(encrypted) < IVSize+aes.BlockSize {
		// Block too short to be encrypted, return as-is (legacy unencrypted block)
		return encrypted, nil
	}

	ciphertextLen := len(encrypted) - IVSize
	if ciphertextLen%aes.BlockSize != 0 {
		// Ciphertext length is not a multiple of block size - not encrypted
		// Return as-is (legacy unencrypted block)
		return encrypted, nil
	}

	// Extract IV from first 16 bytes
	iv := encrypted[:IVSize]
	ciphertext := encrypted[IVSize:]

	block, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Decrypt with CBC
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS7 padding
	unpadded, err := pkcs7Unpad(plaintext)
	if err != nil {
		// Padding error could mean block wasn't actually encrypted
		// Return original data (legacy unencrypted block)
		return encrypted, nil
	}

	return unpadded, nil
}

// GetFileKeyFromPassword derives the file key from a password for an encrypted library.
// This decrypts the random_key using the password-derived key.
func GetFileKeyFromPassword(password, repoID string, salt []byte, randomKey string, version int) ([]byte, error) {
	var derivedKey, iv []byte

	if version == EncVersionDual || version == EncVersionSeafileV2 {
		// Use PBKDF2 with static salt for Seafile compatibility
		derivedKey, iv = DeriveKeyPBKDF2(password, repoID, nil, EncVersionSeafileV2)
	} else if version >= EncVersionSeafileV4 {
		derivedKey, iv = DeriveKeyPBKDF2(password, repoID, salt, version)
	} else {
		return nil, fmt.Errorf("unsupported encryption version: %d", version)
	}

	// Decrypt the random_key to get the file key
	fileKey, err := DecryptFileKey(randomKey, derivedKey, iv)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt file key: %w", err)
	}

	return fileKey, nil
}
