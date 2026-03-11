package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func GenerateTenantToken(secret, issuer, tenantID string, ttl time.Duration) (string, error) {
	secret = strings.TrimSpace(secret)
	issuer = strings.TrimSpace(issuer)
	tenantID = strings.TrimSpace(tenantID)

	if secret == "" {
		return "", fmt.Errorf("secret is required")
	}
	if tenantID == "" {
		return "", fmt.Errorf("tenant_id is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	now := time.Now().UTC()
	claims := TenantClaims{
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}
