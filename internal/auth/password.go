package auth

import (
	"errors"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordLen = 10
	maxPasswordLen = 128
)

// ErrPasswordTooShort is returned when password is too short.
var ErrPasswordTooShort = errors.New("password must be at least 10 characters")

// ErrPasswordTooLong is returned when password exceeds max length.
var ErrPasswordTooLong = errors.New("password must not exceed 128 characters")

// ErrPasswordMissingLetter is returned when password lacks a letter.
var ErrPasswordMissingLetter = errors.New("password must contain at least one letter")

// ErrPasswordMissingDigit is returned when password lacks a digit.
var ErrPasswordMissingDigit = errors.New("password must contain at least one digit")

// HashPassword hashes a password using bcrypt with cost=12.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

// CheckPassword verifies a password against a bcrypt hash.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// ValidatePassword enforces password policy:
// - Min 10 chars, max 128 chars
// - Must contain at least 1 letter and 1 digit
// - Leading/trailing whitespace is trimmed before validation
func ValidatePassword(password string) error {
	password = strings.TrimSpace(password)

	if len(password) < minPasswordLen {
		return ErrPasswordTooShort
	}
	if len(password) > maxPasswordLen {
		return ErrPasswordTooLong
	}

	hasLetter := false
	hasDigit := false
	for _, ch := range password {
		if unicode.IsLetter(ch) {
			hasLetter = true
		}
		if unicode.IsDigit(ch) {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			break
		}
	}

	if !hasLetter {
		return ErrPasswordMissingLetter
	}
	if !hasDigit {
		return ErrPasswordMissingDigit
	}

	return nil
}

func IsPasswordPolicyError(err error) bool {
	return errors.Is(err, ErrPasswordTooShort) ||
		errors.Is(err, ErrPasswordTooLong) ||
		errors.Is(err, ErrPasswordMissingLetter) ||
		errors.Is(err, ErrPasswordMissingDigit)
}
