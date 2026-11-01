package main

import (
	"encoding/json"
	"flag"
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

func main() {
	privateKeyPath := flag.String(
		"private-key",
		"./jwt_private.pem",
		"path to Portal private key",
	)

	merchantID := flag.String(
		"merchant-id",
		"MRC-2026-001",
		"merchant ID",
	)

	userID := flag.String(
		"user-id",
		"merchant-user-123",
		"merchant user ID",
	)

	issuer := flag.String(
		"issuer",
		"portal-merchant-onboarding",
		"JWT issuer",
	)

	audience := flag.String(
		"audience",
		"dms-service",
		"JWT audience",
	)

	expiry := flag.Duration(
		"expiry",
		time.Hour,
		"token expiry duration",
	)

	flag.Parse()

	privateKeyPEM, err := os.ReadFile(*privateKeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read private key: %v\n", err)
		os.Exit(1)
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse private key: %v\n", err)
		os.Exit(1)
	}

	now := time.Now()

	claims := MerchantClaims{
		MerchantID: *merchantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   *userID,
			Issuer:    *issuer,
			Audience:  jwt.ClaimStrings{*audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(*expiry)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign token: %v\n", err)
		os.Exit(1)
	}

	output, _ := json.Marshal(map[string]string{
		"accessToken": tokenString,
		"tokenType":   "Bearer",
		"expiresIn":   fmt.Sprintf("%d", int(expiry.Seconds())),
	})

	fmt.Println(string(output))
}
