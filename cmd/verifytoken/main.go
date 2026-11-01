package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type MerchantClaims struct {
	MerchantID string `json:"merchant_id"`
	jwt.RegisteredClaims
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage:")
		fmt.Println("go run ./cmd/verifytoken <public-key-path> <token>")
		os.Exit(1)
	}

	publicKeyPath := os.Args[1]
	tokenString := os.Args[2]

	publicKeyPEM, err := os.ReadFile(publicKeyPath)
	if err != nil {
		fmt.Printf("GAGAL MEMBACA PUBLIC KEY: %v\n", err)
		os.Exit(1)
	}

	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(publicKeyPEM)
	if err != nil {
		fmt.Printf("GAGAL PARSE PUBLIC KEY: %v\n", err)
		os.Exit(1)
	}

	claims := &MerchantClaims{}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (interface{}, error) {
			return publicKey, nil
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer("portal-merchant-onboarding"),
		jwt.WithAudience("dms-service"),
		jwt.WithExpirationRequired(),
	)

	fmt.Println("TOKEN VALID:", token != nil && token.Valid)

	if err != nil {
		fmt.Printf("ERROR VALIDASI: %v\n", err)

		if errors.Is(err, jwt.ErrTokenExpired) {
			fmt.Println("PENYEBAB: token expired")
		}

		if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			fmt.Println("PENYEBAB: private key dan public key tidak berpasangan")
		}

		if errors.Is(err, jwt.ErrTokenInvalidIssuer) {
			fmt.Println("PENYEBAB: issuer tidak cocok")
		}

		if errors.Is(err, jwt.ErrTokenInvalidAudience) {
			fmt.Println("PENYEBAB: audience tidak cocok")
		}

		os.Exit(1)
	}

	fmt.Printf("Subject     : %s\n", claims.Subject)
	fmt.Printf("Merchant ID : %s\n", claims.MerchantID)
	fmt.Printf("Issuer      : %s\n", claims.Issuer)
	fmt.Printf("Audience    : %v\n", claims.Audience)
	fmt.Printf("Issued At   : %s\n", claims.IssuedAt.Time.Format(time.RFC3339))
	fmt.Printf("Expires At  : %s\n", claims.ExpiresAt.Time.Format(time.RFC3339))
}
