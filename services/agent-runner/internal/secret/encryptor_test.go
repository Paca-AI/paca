package secret

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := DecodeHexKey(repeat("ab", 32))
	if err != nil {
		t.Fatalf("DecodeHexKey: %v", err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	ciphertext, err := enc.Encrypt("sk-super-secret-llm-key")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "sk-super-secret-llm-key" {
		t.Errorf("plaintext = %q, want sk-super-secret-llm-key", plaintext)
	}
}

// TestDecryptCiphertextFromAPIService is a fixed cross-service vector: the
// ciphertext below was produced by services/api's own
// internal/platform/secret.Encryptor (not this package — that package is
// unimportable here across the module boundary, see this package's doc
// comment), encrypting "sk-super-secret-llm-key" with the all-0xab key
// below. Both processes share one ENCRYPTION_KEY in production, so a value
// services/api encrypts (agents.llm_api_key_secret) must decrypt correctly
// here. This pins that contract — a future edit to either Encrypt/Decrypt
// that isn't mirrored on both sides would break this test rather than fail
// silently in production.
func TestDecryptCiphertextFromAPIService(t *testing.T) {
	key, err := DecodeHexKey(repeat("ab", 32))
	if err != nil {
		t.Fatalf("DecodeHexKey: %v", err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	const ciphertextFromAPIService = "jrOMZhxAIsd8AVVZo7r56g1z5pFq6T8ADleKF/uSaOKcsI64LeW2nuu4AS8ODnC+nlrX"

	plaintext, err := enc.Decrypt(ciphertextFromAPIService)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "sk-super-secret-llm-key" {
		t.Errorf("plaintext = %q, want sk-super-secret-llm-key", plaintext)
	}
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
