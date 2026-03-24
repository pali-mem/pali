package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuthenticator validates HMAC-signed tenant tokens.
type JWTAuthenticator struct {
	secret []byte
	issuer string
}

// NewJWTAuthenticator constructs a JWT authenticator.
func NewJWTAuthenticator(secret, issuer string) (*JWTAuthenticator, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("jwt secret is required")
	}
	return &JWTAuthenticator{
		secret: []byte(secret),
		issuer: strings.TrimSpace(issuer),
	}, nil
}

// Authenticate validates a token and returns its tenant ID.
func (a *JWTAuthenticator) Authenticate(ctx context.Context, token string) (string, error) {
	_ = ctx
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("unauthorized")
	}

	claims := &TenantClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil || !parsed.Valid {
		return "", errors.New("unauthorized")
	}

	if strings.TrimSpace(claims.TenantID) == "" {
		return "", errors.New("unauthorized")
	}
	if a.issuer != "" && claims.Issuer != "" && claims.Issuer != a.issuer {
		return "", errors.New("unauthorized")
	}

	return claims.TenantID, nil
}
