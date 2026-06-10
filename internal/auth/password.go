package auth

import "errors"

var (
	// ErrPasswordTooShort is returned when a password has fewer than 12 runes.
	ErrPasswordTooShort = errors.New("password must be at least 12 characters")
	// ErrPasswordTooLong is returned when a password exceeds 72 bytes
	// (bcrypt silently truncates beyond this limit).
	ErrPasswordTooLong = errors.New("password must be at most 72 bytes")
)

// ValidatePassword enforces the project password policy:
//   - minimum 12 runes (character count, not byte length)
//   - maximum 72 bytes (bcrypt hard truncation boundary)
//
// An empty password is rejected by the minimum-length check.
func ValidatePassword(s string) error {
	if len([]rune(s)) < 12 {
		return ErrPasswordTooShort
	}
	if len([]byte(s)) > 72 {
		return ErrPasswordTooLong
	}
	return nil
}
