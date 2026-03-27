package auth

import "testing"

func TestAuthErrors_AreDefined(t *testing.T) {
	if ErrInvalidCredentials == nil {
		t.Fatal("ErrInvalidCredentials must be defined")
	}
	if ErrTokenInvalid == nil {
		t.Fatal("ErrTokenInvalid must be defined")
	}
	if ErrSessionInvalidated == nil {
		t.Fatal("ErrSessionInvalidated must be defined")
	}
}
