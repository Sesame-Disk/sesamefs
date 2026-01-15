package crypto

import (
    "encoding/hex"
    "testing"
)

// TestSeafileCompatibility verifies our PBKDF2 implementation matches Seafile's
// This uses known test vectors computed with Python/Seafile's algorithm
func TestSeafileCompatibility(t *testing.T) {
    // Test case 1: Standard repo_id with hyphens
    repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"
    password := "TestPassword123"
    
    // Expected value computed with Python using Seafile's algorithm:
    // input = repo_id + password
    // salt = {0xda, 0x90, 0x45, 0xc3, 0x06, 0xc7, 0xcc, 0x26}
    // magic = hex(PBKDF2-SHA256(input, salt, 1000, 32))
    expectedMagic := "3f6e4fd28b8e1df2a74ea2f8b781549a8a0a9040fcedff7b85b5a9305aede523"
    
    computed := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
    
    t.Logf("repo_id: %s", repoID)
    t.Logf("password: %s", password)
    t.Logf("computed magic: %s", computed)
    t.Logf("expected magic: %s", expectedMagic)
    
    if computed != expectedMagic {
        t.Errorf("Magic mismatch!\nComputed: %s\nExpected: %s", computed, expectedMagic)
    }
}

// TestRandomKeyFormat verifies our encrypted random_key format matches Seafile's
func TestRandomKeyFormat(t *testing.T) {
    password := "TestPassword123"
    repoID := "543f7a13-7145-4d85-a768-8c91755cfb77"

    params, err := CreateEncryptedLibrary(password, repoID)
    if err != nil {
        t.Fatalf("CreateEncryptedLibrary failed: %v", err)
    }

    t.Logf("RandomKey hex length: %d", len(params.RandomKey))
    t.Logf("RandomKey: %s", params.RandomKey)

    // Seafile's random_key is:
    // - 32-byte file key
    // - Encrypted with AES-256-CBC
    // - PKCS7 padding adds up to 16 bytes (32 -> 48 bytes)
    // - Hex-encoded: 48 * 2 = 96 chars
    expectedLength := 96
    if len(params.RandomKey) != expectedLength {
        t.Errorf("RandomKey length mismatch: got %d, expected %d", len(params.RandomKey), expectedLength)
    }

    // Verify we can decrypt it back
    // CRITICAL: random_key uses password only (not repo_id + password)
    key, iv := DeriveEncryptionKeyPBKDF2(password, nil, EncVersionSeafileV2)
    decryptedKey, err := DecryptFileKey(params.RandomKey, key, iv)
    if err != nil {
        t.Errorf("Failed to decrypt random_key: %v", err)
    }
    t.Logf("Decrypted file key length: %d", len(decryptedKey))

    if len(decryptedKey) != 32 {
        t.Errorf("Decrypted file key wrong length: got %d, expected 32", len(decryptedKey))
    }
}

// TestActualLibraryMagic tests against a real library in the database
func TestActualLibraryMagic(t *testing.T) {
    repoID := "1596b6b2-376a-44d3-bbf5-8768410b11b9"
    storedMagic := "9a828d3b5210520c0a087bf96b4a3518485e27dabf41dc1be3344ea8a17a9c9b"
    
    // Try common test passwords
    passwords := []string{
        "test",
        "test123",
        "password",
        "password123",
        "123456",
        "12345678",
        "testpassword",
        "EncTest",
        "StaticSalt",
        "EncTest-StaticSalt",
        "seafile",
    }
    
    for _, pwd := range passwords {
        computed := ComputeMagicSeafile(pwd, repoID, nil, EncVersionSeafileV2)
        if computed == storedMagic {
            t.Logf("FOUND! Password '%s' produces correct magic", pwd)
            return
        }
    }
    
    t.Logf("No matching password found. Stored magic: %s", storedMagic)
    t.Logf("Testing with 'test123':")
    testMagic := ComputeMagicSeafile("test123", repoID, nil, EncVersionSeafileV2)
    t.Logf("  Computed: %s", testMagic)
}

// TestNewLibraryMagic verifies magic computation for a newly created library
func TestNewLibraryMagic(t *testing.T) {
    repoID := "b2d34167-1609-43ca-8113-96da94d67fdb"
    password := "myTestPassword123"

    // What our implementation computes
    computed := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)

    t.Logf("repo_id: %s", repoID)
    t.Logf("password: %s", password)
    t.Logf("computed:     %s", computed)

    // What the Python/Seafile reference implementation computes
    // input = repo_id + password, salt = static, iterations = 1000
    expectedByRef := "824e3cbfd79d45a5a80f48840f5ae39f8cb9df654a634a735f94eba1934b9ab7"
    t.Logf("Python ref:   %s", expectedByRef)

    if computed == expectedByRef {
        t.Log("Our Go implementation matches Python reference")
    } else {
        t.Errorf("Our Go implementation does NOT match Python reference!")
    }

    // Debug: print what key derivation actually uses
    t.Logf("\nDebug DeriveKeyPBKDF2:")
    t.Logf("  static salt: %x", seafileStaticSalt)
    t.Logf("  input: %s", repoID+password)
}

// TestCreateEncryptedLibraryMagicConsistency verifies CreateEncryptedLibrary uses correct algorithm
func TestCreateEncryptedLibraryMagicConsistency(t *testing.T) {
    password := "myTestPassword123"
    repoID := "b2d34167-1609-43ca-8113-96da94d67fdb"
    
    // Create encrypted library
    params, err := CreateEncryptedLibrary(password, repoID)
    if err != nil {
        t.Fatalf("CreateEncryptedLibrary failed: %v", err)
    }
    
    // Compute magic directly
    directMagic := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
    
    t.Logf("CreateEncryptedLibrary Magic: %s", params.Magic)
    t.Logf("Direct ComputeMagicSeafile:   %s", directMagic)
    
    if params.Magic != directMagic {
        t.Errorf("MISMATCH! CreateEncryptedLibrary is using different algorithm!")
    }
    
    // Compute with Python reference algorithm
    expectedByRef := "824e3cbfd79d45a5a80f48840f5ae39f8cb9df654a634a735f94eba1934b9ab7"
    t.Logf("Python reference:             %s", expectedByRef)
    
    if directMagic == expectedByRef {
        t.Log("Direct computation matches Python reference - algorithm is correct")
    } else {
        t.Error("Direct computation does NOT match Python - algorithm is wrong!")
    }
}

// TestSpecificRepoMagic tests magic for a specific repo
func TestSpecificRepoMagic(t *testing.T) {
    repoID := "d5d6477b-d28f-4264-9139-d411f30c9a82"
    password := "testPwd456"

    // What our Go implementation computes
    goMagic := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)

    // What Python/Seafile computes (using static salt)
    expectedPython := "717a5d923712663deaaab0cba251253c8eb8bcad5a6c84bb16a86c8c2f8cb293"

    t.Logf("repo_id: %s", repoID)
    t.Logf("password: %s", password)
    t.Logf("Go computed:     %s", goMagic)
    t.Logf("Python expected: %s", expectedPython)

    if goMagic == expectedPython {
        t.Log("Go matches Python - algorithm is correct")
    } else {
        t.Error("Go does NOT match Python!")
    }
}

// TestAPIBehavior simulates what the API does step by step
func TestAPIBehavior(t *testing.T) {
    password := "debugPwd123"
    repoID := "75032973-ad3c-46cc-8cdb-34cbba09f428"

    // Step 1: What CreateEncryptedLibrary would compute
    params, err := CreateEncryptedLibrary(password, repoID)
    if err != nil {
        t.Fatalf("CreateEncryptedLibrary failed: %v", err)
    }
    t.Logf("CreateEncryptedLibrary returned magic: %s", params.Magic)

    // Step 2: What ComputeMagicSeafile directly computes
    directMagic := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
    t.Logf("Direct ComputeMagicSeafile: %s", directMagic)

    // The API magic and directMagic should match
    if params.Magic == directMagic {
        t.Log("CreateEncryptedLibrary magic MATCHES direct computation - algorithm is consistent")
    } else {
        t.Error("CreateEncryptedLibrary magic does NOT match direct computation!")
    }

    // Verify we can decrypt the random_key
    encKey, encIV := DeriveEncryptionKeyPBKDF2(password, nil, EncVersionSeafileV2)
    secretKey, err := DecryptFileKey(params.RandomKey, encKey, encIV)
    if err != nil {
        t.Errorf("Failed to decrypt RandomKey: %v", err)
    } else {
        t.Logf("Decrypted secret key: %x", secretKey)
        if len(secretKey) != 32 {
            t.Errorf("Secret key should be 32 bytes, got %d", len(secretKey))
        }
    }
}

// TestMagicWithDifferentSalts checks if API might be using wrong salt
func TestMagicWithDifferentSalts(t *testing.T) {
    password := "debugPwd123"
    repoID := "75032973-ad3c-46cc-8cdb-34cbba09f428"
    apiMagic := "f554c65f2d93a65dcb5643f4d34d8fc8ade6d3c29a9418b6c3e24334ede2773c"
    apiSaltHex := "50aff25a9f502666e2b2f4e2496b250b742cc2275d1950bbd91f64f8bd2e6952"
    
    // 1. Magic with static salt (correct for Seafile v2)
    magicStatic := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
    t.Logf("Magic with static salt:       %s", magicStatic)
    
    // 2. Magic with the random salt from API (wrong, but let's check)
    apiSalt, _ := hex.DecodeString(apiSaltHex)
    magicWithRandomSalt := ComputeMagicSeafile(password, repoID, apiSalt, EncVersionSeafileV4)
    t.Logf("Magic with random salt (v4):  %s", magicWithRandomSalt)
    
    // 3. What if we're using Argon2id salt for PBKDF2?
    keyPBKDF2, _ := DeriveKeyPBKDF2(password, repoID, apiSalt, EncVersionSeafileV4)
    magicFromPBKDF2WithSalt := hex.EncodeToString(keyPBKDF2)
    t.Logf("PBKDF2 with random salt:      %s", magicFromPBKDF2WithSalt)
    
    t.Logf("API magic:                    %s", apiMagic)
    
    if magicStatic == apiMagic {
        t.Log("MATCH: Static salt")
    } else if magicWithRandomSalt == apiMagic {
        t.Log("MATCH: Random salt with v4 - BUG: using random salt instead of static!")
    } else if magicFromPBKDF2WithSalt == apiMagic {
        t.Log("MATCH: PBKDF2 with random salt - same as v4")
    } else {
        t.Log("No match found - need to investigate further")
        
        // Try Argon2id magic
        keyArgon2, _ := DeriveKeyArgon2id(password, repoID, apiSalt)
        magicArgon2 := ComputeMagic(repoID, keyArgon2)
        t.Logf("Argon2id magic (MagicStrong): %s", magicArgon2)
        if magicArgon2 == apiMagic {
            t.Error("MATCH: Argon2id magic - BUG: API is returning MagicStrong instead of Magic!")
        }
    }
}

// TestReferenceServerDecryption tests against the actual reference Seafile server data
func TestReferenceServerDecryption(t *testing.T) {
    // Data from reference Seafile server (app.nihaoconsult.com)
    repoID := "256b7b88-d9cf-44d1-ba46-a5bb0bf0ebf7"
    password := "testcli123"
    storedMagic := "eee7b4a7a2539f2e4fb1c88a40121077152a5dc2c223f607f8b5e0838affde61"
    randomKey := "406ec194a0d0f7985b831b040034d829a4c68fbd354982f58101d0b6edd0232efed903cd1c404d0a66892473be968f19"

    // Client stored these keys after successful sync
    expectedFinalKey := "7c883d20553fbdf782e74e0eb58abccc33a053113db50c2e56220ce954e4d36f"
    expectedFinalIV := "242e17f1ea5278b84343d8834a3aab3d"

    // Step 1: Verify magic matches (uses repo_id + password)
    computed := ComputeMagicSeafile(password, repoID, nil, EncVersionSeafileV2)
    t.Logf("Step 1 - Magic verification:")
    t.Logf("  Stored:   %s", storedMagic)
    t.Logf("  Computed: %s", computed)
    if computed != storedMagic {
        t.Errorf("Magic mismatch!")
    } else {
        t.Log("  Magic matches!")
    }

    // Step 2: Decrypt random_key
    // CRITICAL: random_key uses PASSWORD ONLY (not repo_id + password)
    key, iv := DeriveEncryptionKeyPBKDF2(password, nil, EncVersionSeafileV2)
    t.Logf("\nStep 2 - Derived key/IV for random_key decryption (password only):")
    t.Logf("  Key: %s", hex.EncodeToString(key))
    t.Logf("  IV:  %s", hex.EncodeToString(iv))

    secretKey, err := DecryptFileKey(randomKey, key, iv)
    if err != nil {
        t.Errorf("ERROR decrypting random_key: %v", err)
        t.Logf("  raw random_key bytes: %s", randomKey)
        enc, _ := hex.DecodeString(randomKey)
        t.Logf("  encrypted length: %d bytes", len(enc))
    } else {
        t.Logf("  Secret key: %s", hex.EncodeToString(secretKey))

        // Step 3: Derive final key/IV from secret key
        finalKey, finalIV := DeriveFileEncryptionKey(secretKey, EncVersionSeafileV2)
        t.Logf("\nStep 3 - Final file encryption key/IV:")
        t.Logf("  Final key: %s", hex.EncodeToString(finalKey))
        t.Logf("  Final IV:  %s", hex.EncodeToString(finalIV))
        t.Logf("  Expected key: %s", expectedFinalKey)
        t.Logf("  Expected IV:  %s", expectedFinalIV)

        if hex.EncodeToString(finalKey) == expectedFinalKey {
            t.Log("  Final key MATCHES!")
        } else {
            t.Error("  Final key does NOT match!")
        }

        if hex.EncodeToString(finalIV) == expectedFinalIV {
            t.Log("  Final IV MATCHES!")
        } else {
            t.Error("  Final IV does NOT match!")
        }
    }
}
