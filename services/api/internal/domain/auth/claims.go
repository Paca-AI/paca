// Package auth holds JWT claims and the auth service contract.
package auth

import "github.com/golang-jwt/jwt/v5"

// Claims is the payload embedded in every JWT issued by the service.
type Claims struct {
	jwt.RegisteredClaims
	Username string `json:"username"`
	Role     string `json:"role"`
	// Kind distinguishes "access" from "refresh" tokens.
	Kind string `json:"kind"`
	// FamilyID links all tokens that originate from the same login session.
	// Used for refresh-token rotation and reuse detection.
	FamilyID string `json:"fid"`
	// RememberMe is true when the user opted into a persistent session.
	// Propagated through refresh-token rotation to preserve the original
	// session lifetime preference.
	RememberMe bool `json:"rme"`
}
