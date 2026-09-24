package jev

import "github.com/Paca-AI/api/internal/platform/secret"

// ClientForProject builds a Client from a project's stored Jev credentials
// (see projectdom.Project.JevAPIKeySecret's doc comment). apiKeySecret is
// the value stored in the database — encrypted if enc is non-nil, plaintext
// otherwise (enc is nil exactly when the instance has no ENCRYPTION_KEY
// configured, matching every other secret in this codebase — see
// service/project's encryptJevKey).
//
// Returns nil (Enabled() reports false) when the project hasn't configured
// Jev or the stored key fails to decrypt — every caller in this codebase
// treats "no client" as "skip this Jev-dependent step", never a hard error.
func ClientForProject(apiKeySecret, baseURL, model string, enc *secret.Encryptor) *Client {
	if apiKeySecret == "" {
		return nil
	}
	apiKey := apiKeySecret
	if enc != nil {
		decrypted, err := enc.Decrypt(apiKeySecret)
		if err != nil {
			return nil
		}
		apiKey = decrypted
	}
	return New(apiKey, baseURL, model)
}
