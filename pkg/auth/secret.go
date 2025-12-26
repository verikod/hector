package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
)

// SecretValidator validates tokens against a shared secret.
type SecretValidator struct {
	secret string
}

// NewSecretValidator creates a new SecretValidator.
func NewSecretValidator(secret string) *SecretValidator {
	return &SecretValidator{
		secret: secret,
	}
}

// ValidateToken validates the token against the shared secret.
// It uses constant-time comparison to prevent timing attacks.
func (v *SecretValidator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	// constant-time comparison
	if subtle.ConstantTimeCompare([]byte(tokenString), []byte(v.secret)) != 1 {
		return nil, fmt.Errorf("invalid token")
	}

	// Return admin claims
	return &Claims{
		Subject: "admin-via-secret",
		Role:    "admin",
		Custom: map[string]any{
			"auth_type": "shared_secret",
		},
	}, nil
}

// Close is a no-op for SecretValidator.
func (v *SecretValidator) Close() error {
	return nil
}

// Ensure SecretValidator implements TokenValidator
var _ TokenValidator = (*SecretValidator)(nil)
