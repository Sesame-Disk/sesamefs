package crypto

import (
	"encoding/hex"
	"testing"
)

func TestGenerateSalt(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}
	if len(salt) != SaltSize {
		t.Errorf("expected salt length %d, got %d", SaltSize, len(salt))
	}

	// Generate another salt, should be different
	salt2, _ := GenerateSalt()
	if hex.EncodeToString(salt) == hex.EncodeToString(salt2) {
		t.Error("two salts should not be identical")
	}
}

func TestGenerateFileKey(t *testing.T) {
	key, err := GenerateFileKey()
	if err != nil {
		t.Fatalf("GenerateFileKey failed: %v", err)
	}
	if len(key) != FileKeySize {
		t.Errorf("expected key length %d, got %d", FileKeySize, len(key))
	}
}

func TestDeriveKeyPBKDF2(t *testing.T) {
	password := "TestPassword123"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"
	salt, _ := GenerateSalt()

	key, iv := DeriveKeyPBKDF2(password, repoID, salt, EncVersionSeafileV2)

	if len(key) != pbkdf2KeyLen {
		t.Errorf("expected key length %d, got %d", pbkdf2KeyLen, len(key))
	}
	if len(iv) != IVSize {
		t.Errorf("expected IV length %d, got %d", IVSize, len(iv))
	}

	// Same inputs should produce same outputs
	key2, iv2 := DeriveKeyPBKDF2(password, repoID, salt, EncVersionSeafileV2)
	if hex.EncodeToString(key) != hex.EncodeToString(key2) {
		t.Error("same inputs should produce same key")
	}
	if hex.EncodeToString(iv) != hex.EncodeToString(iv2) {
		t.Error("same inputs should produce same IV")
	}

	// Different password should produce different key
	key3, _ := DeriveKeyPBKDF2("DifferentPassword", repoID, salt, EncVersionSeafileV2)
	if hex.EncodeToString(key) == hex.EncodeToString(key3) {
		t.Error("different passwords should produce different keys")
	}
}

func TestDeriveKeyArgon2id(t *testing.T) {
	password := "TestPassword123"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"
	salt, _ := GenerateSalt()

	key, iv := DeriveKeyArgon2id(password, repoID, salt)

	if len(key) != argon2KeyLen {
		t.Errorf("expected key length %d, got %d", argon2KeyLen, len(key))
	}
	if len(iv) != IVSize {
		t.Errorf("expected IV length %d, got %d", IVSize, len(iv))
	}

	// Same inputs should produce same outputs
	key2, iv2 := DeriveKeyArgon2id(password, repoID, salt)
	if hex.EncodeToString(key) != hex.EncodeToString(key2) {
		t.Error("same inputs should produce same key")
	}
	if hex.EncodeToString(iv) != hex.EncodeToString(iv2) {
		t.Error("same inputs should produce same IV")
	}
}

func TestComputeMagic(t *testing.T) {
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	magic := ComputeMagic(repoID, key)

	// Should be 64 hex chars (32 bytes)
	if len(magic) != 64 {
		t.Errorf("expected magic length 64, got %d", len(magic))
	}

	// Same inputs should produce same magic
	magic2 := ComputeMagic(repoID, key)
	if magic != magic2 {
		t.Error("same inputs should produce same magic")
	}
}

func TestVerifyPassword(t *testing.T) {
	if !VerifyPassword("abc123", "abc123") {
		t.Error("identical strings should verify")
	}
	if VerifyPassword("abc123", "abc124") {
		t.Error("different strings should not verify")
	}
	if VerifyPassword("abc123", "ABC123") {
		t.Error("case-different strings should not verify")
	}
}

func TestEncryptDecryptFileKey(t *testing.T) {
	fileKey, _ := GenerateFileKey()
	derivedKey := make([]byte, 32)
	iv := make([]byte, 16)
	for i := range derivedKey {
		derivedKey[i] = byte(i)
	}
	for i := range iv {
		iv[i] = byte(i + 100)
	}

	encrypted, err := EncryptFileKey(fileKey, derivedKey, iv)
	if err != nil {
		t.Fatalf("EncryptFileKey failed: %v", err)
	}

	decrypted, err := DecryptFileKey(encrypted, derivedKey, iv)
	if err != nil {
		t.Fatalf("DecryptFileKey failed: %v", err)
	}

	if hex.EncodeToString(fileKey) != hex.EncodeToString(decrypted) {
		t.Error("decrypted key should match original")
	}
}

func TestCreateEncryptedLibrary(t *testing.T) {
	password := "TestPassword123"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"

	params, err := CreateEncryptedLibrary(password, repoID)
	if err != nil {
		t.Fatalf("CreateEncryptedLibrary failed: %v", err)
	}

	if params.EncVersion != EncVersionDual {
		t.Errorf("expected enc_version %d, got %d", EncVersionDual, params.EncVersion)
	}

	// Salt should be 64 hex chars (32 bytes)
	if len(params.Salt) != 64 {
		t.Errorf("expected salt length 64, got %d", len(params.Salt))
	}

	// Magic should be 64 hex chars
	if len(params.Magic) != 64 {
		t.Errorf("expected magic length 64, got %d", len(params.Magic))
	}

	// MagicStrong should be 64 hex chars
	if len(params.MagicStrong) != 64 {
		t.Errorf("expected magic_strong length 64, got %d", len(params.MagicStrong))
	}

	// RandomKey should be valid hex
	if _, err := hex.DecodeString(params.RandomKey); err != nil {
		t.Errorf("random_key is not valid hex: %v", err)
	}

	// Verify password works
	salt, _ := hex.DecodeString(params.Salt)
	if !VerifyPasswordSeafile(password, repoID, params.Magic, salt, params.EncVersion) {
		t.Error("password verification (Seafile) failed")
	}
	if !VerifyPasswordStrong(password, repoID, params.MagicStrong, salt) {
		t.Error("password verification (Strong) failed")
	}

	// Wrong password should fail
	if VerifyPasswordSeafile("WrongPassword", repoID, params.Magic, salt, params.EncVersion) {
		t.Error("wrong password should not verify (Seafile)")
	}
	if VerifyPasswordStrong("WrongPassword", repoID, params.MagicStrong, salt) {
		t.Error("wrong password should not verify (Strong)")
	}
}

func TestChangePassword(t *testing.T) {
	oldPassword := "OldPassword123"
	newPassword := "NewPassword456"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"

	// Create initial encrypted library
	params, err := CreateEncryptedLibrary(oldPassword, repoID)
	if err != nil {
		t.Fatalf("CreateEncryptedLibrary failed: %v", err)
	}

	// Decrypt file key with old password for comparison
	// CRITICAL: RandomKey uses PASSWORD ONLY (not repo_id + password)
	oldKey, oldIV := DeriveEncryptionKeyPBKDF2(oldPassword, nil, EncVersionSeafileV2)
	originalFileKey, _ := DecryptFileKey(params.RandomKey, oldKey, oldIV)

	// Change password
	newParams, err := ChangePassword(oldPassword, newPassword, repoID, params)
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// Magic should be different
	if params.Magic == newParams.Magic {
		t.Error("magic should change with new password")
	}

	// Salt should be different
	if params.Salt == newParams.Salt {
		t.Error("salt should change with new password")
	}

	// Verify new password works
	newSalt, _ := hex.DecodeString(newParams.Salt)
	if !VerifyPasswordSeafile(newPassword, repoID, newParams.Magic, newSalt, newParams.EncVersion) {
		t.Error("new password verification (Seafile) failed")
	}
	if !VerifyPasswordStrong(newPassword, repoID, newParams.MagicStrong, newSalt) {
		t.Error("new password verification (Strong) failed")
	}

	// Old password should not work
	if VerifyPasswordSeafile(oldPassword, repoID, newParams.Magic, newSalt, newParams.EncVersion) {
		t.Error("old password should not work after change")
	}

	// Decrypt file key with new password - should be same as original
	// CRITICAL: RandomKey uses PASSWORD ONLY (not repo_id + password)
	newKey, newIV := DeriveEncryptionKeyPBKDF2(newPassword, nil, EncVersionSeafileV2)
	decryptedFileKey, err := DecryptFileKey(newParams.RandomKey, newKey, newIV)
	if err != nil {
		t.Fatalf("failed to decrypt file key with new password: %v", err)
	}

	if hex.EncodeToString(originalFileKey) != hex.EncodeToString(decryptedFileKey) {
		t.Error("file key should remain the same after password change")
	}
}

func TestChangePasswordWrongOld(t *testing.T) {
	password := "CorrectPassword"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"

	params, _ := CreateEncryptedLibrary(password, repoID)

	_, err := ChangePassword("WrongPassword", "NewPassword", repoID, params)
	if err == nil {
		t.Error("ChangePassword should fail with wrong old password")
	}
	if err.Error() != "wrong password" {
		t.Errorf("expected 'wrong password' error, got: %v", err)
	}
}

func TestPKCS7Padding(t *testing.T) {
	testCases := []struct {
		input     []byte
		blockSize int
	}{
		{[]byte("hello"), 16},
		{[]byte("exactly16bytes!!"), 16},
		{[]byte(""), 16},
		{[]byte("a"), 16},
	}

	for _, tc := range testCases {
		padded := pkcs7Pad(tc.input, tc.blockSize)
		if len(padded)%tc.blockSize != 0 {
			t.Errorf("padded length %d is not multiple of %d", len(padded), tc.blockSize)
		}

		unpadded, err := pkcs7Unpad(padded)
		if err != nil {
			t.Errorf("pkcs7Unpad failed: %v", err)
		}

		if string(unpadded) != string(tc.input) {
			t.Errorf("unpadded %q != original %q", unpadded, tc.input)
		}
	}
}

// BenchmarkDeriveKeyPBKDF2 shows how fast (weak) PBKDF2 is
func BenchmarkDeriveKeyPBKDF2(b *testing.B) {
	password := "TestPassword123"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"
	salt, _ := GenerateSalt()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DeriveKeyPBKDF2(password, repoID, salt, EncVersionSeafileV2)
	}
}

// BenchmarkDeriveKeyArgon2id shows how much slower (stronger) Argon2id is
func BenchmarkDeriveKeyArgon2id(b *testing.B) {
	password := "TestPassword123"
	repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"
	salt, _ := GenerateSalt()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DeriveKeyArgon2id(password, repoID, salt)
	}
}
