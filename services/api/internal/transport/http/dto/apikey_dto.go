// Package dto provides request/response types for API key endpoints.
package dto

import (
	"time"

	"github.com/google/uuid"

	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
)

// CreateAPIKeyRequest is the body for POST /users/me/api-keys.
type CreateAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at" binding:"omitempty"`
}

// APIKeyResponse is the public representation of an API key.
// The raw key value is NEVER included here; it is returned only in
// CreateAPIKeyResponse on first creation.
type APIKeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateAPIKeyResponse is returned from POST /users/me/api-keys.
// Key contains the full raw key — shown ONCE, never retrievable again.
type CreateAPIKeyResponse struct {
	APIKeyResponse
	Key string `json:"key"`
}

// APIKeyFromEntity maps a domain API key to a transport response.
func APIKeyFromEntity(k *apikeydom.APIKey) APIKeyResponse {
	return APIKeyResponse{
		ID:         k.ID,
		Name:       k.Name,
		KeyPrefix:  k.KeyPrefix,
		LastUsedAt: k.LastUsedAt,
		ExpiresAt:  k.ExpiresAt,
		CreatedAt:  k.CreatedAt,
	}
}
