package auth

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type MerchantClaims struct {
	MerchantID     string `json:"merchant_id"`
	RegistrationID string `json:"registration_id"`
	Role           string `json:"role"`
	jwt.RegisteredClaims
}

type TokenService struct {
	privateKey interface{}
	issuer     string
	audience   string
	expiry     time.Duration
}

func NewTokenService(
	privateKeyPath string,
	issuer string,
	audience string,
	expiry time.Duration,
) (*TokenService, error) {
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read jwt private key: %w", err)
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse jwt private key: %w", err)
	}

	return &TokenService{
		privateKey: privateKey,
		issuer:     issuer,
		audience:   audience,
		expiry:     expiry,
	}, nil
}

func (s *TokenService) IssueToken(
	userID string,
	merchantID string,
	registrationID string,
	role string,
) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.expiry)

	claims := MerchantClaims{
		MerchantID:     merchantID,
		RegistrationID: registrationID,
		Role:           role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	accessToken, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}

	return accessToken, expiresAt, nil
}
