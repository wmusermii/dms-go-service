.PHONY: tidy run build fmt vet

tidy:
	go mod tidy

# Loads .env (copy .env.example first).
run:
	set -a && . ./.env && set +a && go run ./cmd/api

build:
	CGO_ENABLED=0 go build -o bin/dms-api ./cmd/api

fmt:
	gofmt -w .

vet:
	go vet ./...
