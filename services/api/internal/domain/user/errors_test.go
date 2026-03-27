package userdom

import "testing"

func TestUserErrors_AreDefined(t *testing.T) {
	if ErrNotFound == nil {
		t.Fatal("ErrNotFound must be defined")
	}
	if ErrUsernameTaken == nil {
		t.Fatal("ErrUsernameTaken must be defined")
	}
	if ErrForbidden == nil {
		t.Fatal("ErrForbidden must be defined")
	}
}
