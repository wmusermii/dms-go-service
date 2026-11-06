package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
)

func main() {
	randomBytes := make([]byte, 32)

	if _, err := rand.Read(randomBytes); err != nil {
		log.Fatal(err)
	}

	apiKey := "dms_live_" + base64.RawURLEncoding.EncodeToString(randomBytes)

	fmt.Println(apiKey)
}
