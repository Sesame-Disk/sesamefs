package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

func TestDecryptRealBlock(t *testing.T) {
	// NEW library data from debug-enc-test
	password := "debugpass123"
	randomKey := "06634a771ef0521305d0156abc0a3e73a6cc65c2a88d573a4d0d976132a3edd595f91909b7b67c69c3e332a306dce570"

	// The encrypted block from S3 (48 bytes)
	encryptedBlockHex := "7685efdca9c829773 0c7f63749500d15a6cc64efeafb01d6eb567d979fa95dde5197cf7f62385b353d03a9be61c40db6"

	// Clean hex string (remove spaces)
	cleanHex := ""
	for _, c := range encryptedBlockHex {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			cleanHex += string(c)
		}
	}

	encryptedBlock, err := hex.DecodeString(cleanHex)
	if err != nil {
		t.Fatalf("Failed to decode encrypted block: %v", err)
	}

	t.Logf("Encrypted block length: %d bytes", len(encryptedBlock))
	t.Logf("IV: %x", encryptedBlock[:16])

	// Step 1: Get the secretKey (raw key from randomKey)
	encKey, encIV := DeriveEncryptionKeyPBKDF2(password, nil, EncVersionSeafileV2)
	secretKey, err := DecryptFileKey(randomKey, encKey, encIV)
	if err != nil {
		t.Fatalf("DecryptFileKey failed: %v", err)
	}
	t.Logf("secretKey (raw from randomKey): %x", secretKey)

	// Step 2: Get finalKey (with second derivation)
	finalKey, _ := DeriveFileEncryptionKey(secretKey, EncVersionSeafileV2)
	t.Logf("finalKey (with 2nd derivation): %x", finalKey)

	// Try decrypt with secretKey
	t.Log("\nTrying to decrypt with secretKey (no 2nd derivation):")
	decryptedSK, err := DecryptBlock(encryptedBlock, secretKey)
	if err != nil {
		t.Logf("  Failed: %v", err)
	} else {
		t.Logf("  Success! Content: %q", string(decryptedSK))
	}

	// Try decrypt with finalKey
	t.Log("\nTrying to decrypt with finalKey (with 2nd derivation):")
	decryptedFK, err := DecryptBlock(encryptedBlock, finalKey)
	if err != nil {
		t.Logf("  Failed: %v", err)
	} else {
		t.Logf("  Result: %q", string(decryptedFK))
	}

	// Conclusion (note: the shell escaped ! as \! so actual content has backslash)
	expected := "Hello World from debug test\\!\n"
	t.Logf("\nExpected bytes: %x", []byte(expected))
	t.Logf("finalKey decrypted bytes: %x", decryptedFK)

	if string(decryptedSK) == expected {
		t.Log("\n*** The block was encrypted with secretKey (no 2nd derivation)")
		t.Log("*** BUG: SetPassword should NOT do second derivation!")
	} else if string(decryptedFK) == expected {
		t.Log("\n*** The block was encrypted with finalKey (with 2nd derivation)")
		t.Log("*** This is correct behavior - SUCCESS!")
	} else {
		t.Log("\n*** Comparison failed - checking byte-by-byte...")
		t.Logf("Expected length: %d, Got length: %d", len(expected), len(decryptedFK))
		for i := 0; i < len(expected) && i < len(decryptedFK); i++ {
			if expected[i] != decryptedFK[i] {
				t.Logf("Mismatch at byte %d: expected 0x%02x (%c), got 0x%02x (%c)",
					i, expected[i], expected[i], decryptedFK[i], decryptedFK[i])
			}
		}
	}
}

// TestDerivedIVDecryption tests if Seafile uses derived IV instead of prepended random IV
func TestDerivedIVDecryption(t *testing.T) {
	password := "debugpass123"
	randomKey := "06634a771ef0521305d0156abc0a3e73a6cc65c2a88d573a4d0d976132a3edd595f91909b7b67c69c3e332a306dce570"

	// Our encrypted block (48 bytes = 16 IV + 32 ciphertext)
	encryptedBlockHex := "7685efdca9c829773 0c7f63749500d15a6cc64efeafb01d6eb567d979fa95dde5197cf7f62385b353d03a9be61c40db6"
	cleanHex := ""
	for _, c := range encryptedBlockHex {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			cleanHex += string(c)
		}
	}
	encryptedBlock, _ := hex.DecodeString(cleanHex)

	// Derive the keys
	encKey, encIV := DeriveEncryptionKeyPBKDF2(password, nil, EncVersionSeafileV2)
	secretKey, _ := DecryptFileKey(randomKey, encKey, encIV)
	finalKey, derivedIV := DeriveFileEncryptionKey(secretKey, EncVersionSeafileV2)

	t.Logf("Derived IV from second PBKDF2: %x", derivedIV)
	t.Logf("Prepended IV in block: %x", encryptedBlock[:16])

	// Extract just the ciphertext (without prepended IV)
	ciphertext := encryptedBlock[16:]
	t.Logf("Ciphertext (no IV): %x", ciphertext)

	// Try to decrypt with derived IV instead of prepended IV
	block, _ := aes.NewCipher(finalKey)
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, derivedIV)
	mode.CryptBlocks(plaintext, ciphertext)

	t.Logf("Decrypted with derived IV: %q", string(plaintext))
	t.Logf("Decrypted bytes: %x", plaintext)

	// Check if removing padding gives expected result
	if plaintext[len(plaintext)-1] <= 16 {
		padding := int(plaintext[len(plaintext)-1])
		unpaddedLen := len(plaintext) - padding
		t.Logf("After removing PKCS7 padding: %q", string(plaintext[:unpaddedLen]))
	}
}

// TestActualLibraryKeyDerivation traces exact key derivation for the test library
func TestActualLibraryKeyDerivation(t *testing.T) {
	password := "testpass123"
	repoID := "dda1b069-d911-4b53-a040-04bc281a947d"
	randomKey := "aa62ba19ef67321389542abbb83488592f2133e8badb7a2341a9f97e4d47d65c6135e4b7dfa1b194a681f4245822181b"

	// Step 1: Derive encryption key from password (what the library was created with)
	encKey, encIV := DeriveEncryptionKeyPBKDF2(password, nil, EncVersionSeafileV2)
	t.Logf("Step 1 - Derived encryption key/IV from password:")
	t.Logf("  encKey: %x", encKey)
	t.Logf("  encIV: %x", encIV)

	// Step 2: Decrypt randomKey to get secretKey
	secretKey, err := DecryptFileKey(randomKey, encKey, encIV)
	if err != nil {
		t.Fatalf("DecryptFileKey failed: %v", err)
	}
	t.Logf("Step 2 - Decrypted secretKey: %x", secretKey)

	// Step 3: Second derivation (what Seafile does)
	finalKey, finalIV := DeriveFileEncryptionKey(secretKey, EncVersionSeafileV2)
	t.Logf("Step 3 - Final file key/IV after second derivation:")
	t.Logf("  finalKey: %x", finalKey)
	t.Logf("  finalIV: %x", finalIV)

	// Step 4: What GetFileKeyFromPassword returns
	fileKeyFromFunc, err := GetFileKeyFromPassword(password, repoID, nil, randomKey, EncVersionSeafileV2)
	if err != nil {
		t.Fatalf("GetFileKeyFromPassword failed: %v", err)
	}
	t.Logf("Step 4 - GetFileKeyFromPassword returns: %x", fileKeyFromFunc)

	// Verify they match
	if hex.EncodeToString(finalKey) == hex.EncodeToString(fileKeyFromFunc) {
		t.Log("OK: finalKey matches GetFileKeyFromPassword output")
	} else {
		t.Error("MISMATCH: finalKey != GetFileKeyFromPassword output")
	}

	// Test encryption with secretKey (no second derivation)
	plaintext := []byte("This is a test file for encrypted sync testing - Hello World 123!\n")
	encrypted, err := EncryptBlock(plaintext, secretKey)
	if err != nil {
		t.Fatalf("EncryptBlock with secretKey failed: %v", err)
	}
	t.Logf("Step 5 - Encrypted with secretKey (no 2nd deriv): length=%d", len(encrypted))

	// Try decrypt with secretKey
	decrypted1, err := DecryptBlock(encrypted, secretKey)
	if err != nil {
		t.Logf("  Decrypt with secretKey failed: %v", err)
	} else {
		t.Logf("  Decrypted with secretKey: %q", string(decrypted1))
	}

	// Try decrypt with finalKey
	decrypted2, err := DecryptBlock(encrypted, finalKey)
	if err != nil {
		t.Logf("  Decrypt with finalKey failed: %v", err)
	} else {
		t.Logf("  Decrypted with finalKey: %q", string(decrypted2))
	}

	// Now test: if server encrypted with secretKey but we try to decrypt with finalKey
	t.Logf("\nSimulating server encrypting with secretKey, client decrypting with finalKey:")
	if string(decrypted1) == string(plaintext) {
		t.Log("  Encrypt(secretKey) + Decrypt(secretKey) = SUCCESS")
	}
	if string(decrypted2) == string(plaintext) {
		t.Log("  Encrypt(secretKey) + Decrypt(finalKey) = SUCCESS")
	} else {
		t.Log("  Encrypt(secretKey) + Decrypt(finalKey) = FAIL (this is the bug!)")
	}
}

// TestEncryptDecryptFlow tests the full encrypt/decrypt flow
func TestEncryptDecryptFlow(t *testing.T) {
	password := "testpass123"
	repoID := "test-repo-12345"

	// Step 1: Create encrypted library (simulates what happens on library creation)
	params, err := CreateEncryptedLibrary(password, repoID)
	if err != nil {
		t.Fatalf("CreateEncryptedLibrary failed: %v", err)
	}
	t.Logf("Created library with enc_version: %d", params.EncVersion)

	// Step 2: Get the file key for encryption (what SetPassword does)
	// This is what gets stored in the session
	fileKeyForSession, err := GetFileKeyFromPassword(password, repoID, nil, params.RandomKey, EncVersionSeafileV2)
	if err != nil {
		t.Fatalf("GetFileKeyFromPassword failed: %v", err)
	}
	t.Logf("File key from GetFileKeyFromPassword: %x", fileKeyForSession)

	// Step 3: Encrypt some content (what upload does)
	plaintext := []byte("This is test content for encryption!")
	encrypted, err := EncryptBlock(plaintext, fileKeyForSession)
	if err != nil {
		t.Fatalf("EncryptBlock failed: %v", err)
	}
	t.Logf("Encrypted length: %d", len(encrypted))

	// Step 4: Get file key for decryption (same as step 2, simulating what client does)
	fileKeyForDecrypt, err := GetFileKeyFromPassword(password, repoID, nil, params.RandomKey, EncVersionSeafileV2)
	if err != nil {
		t.Fatalf("GetFileKeyFromPassword for decrypt failed: %v", err)
	}
	t.Logf("File key for decryption: %x", fileKeyForDecrypt)

	// Step 5: Decrypt (what client does)
	decrypted, err := DecryptBlock(encrypted, fileKeyForDecrypt)
	if err != nil {
		t.Fatalf("DecryptBlock failed: %v", err)
	}

	t.Logf("Decrypted: %s", string(decrypted))

	if string(decrypted) != string(plaintext) {
		t.Errorf("Decryption failed!\nExpected: %q\nGot: %q", plaintext, decrypted)
	} else {
		t.Log("SUCCESS: Encrypt/Decrypt round trip works!")
	}
}
