// Package auth holds JWT claims and the auth service contract.
package auth

import "github.com/golang-jwt/jwt/v5"

// Claims is the payload embedded in every JWT issued by the service.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Role  string `json:"role"`
	// Kind distinguishes "access" from "refresh" tokens.
	Kind string `json:"kind"`
}
