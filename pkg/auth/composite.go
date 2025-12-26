package auth

import (
	"context"
	"fmt"
	"strings"
)

// CompositeValidator validates tokens against multiple validators.
// It returns the claims from the first validator that succeeds.
type CompositeValidator struct {
	validators []TokenValidator
}

// NewCompositeValidator creates a new CompositeValidator.
func NewCompositeValidator(validators ...TokenValidator) *CompositeValidator {
	return &CompositeValidator{
		validators: validators,
	}
}

// ValidateToken tries each validator in order.
func (v *CompositeValidator) ValidateToken(ctx context.Context, tokenString string) (*Claims, error) {
	var errs []string
	for _, validator := range v.validators {
		claims, err := validator.ValidateToken(ctx, tokenString)
		if err == nil {
			return claims, nil
		}
		errs = append(errs, err.Error())
	}
	return nil, fmt.Errorf("all validators failed: %s", strings.Join(errs, "; "))
}

// Close closes all validators.
func (v *CompositeValidator) Close() error {
	var errs []string
	for _, validator := range v.validators {
		if err := validator.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to close validators: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Ensure CompositeValidator implements TokenValidator
var _ TokenValidator = (*CompositeValidator)(nil)
