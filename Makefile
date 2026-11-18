.PHONY: migrate run test test-integration lint fmt generate test-mutation

-include .env
-include .env.local
export

migrate:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

run:
	go run ./cmd/trip-service

test:
	@set -eu; coverage=$$(mktemp); trap 'rm -f "$$coverage"' EXIT; \
	go test -race -count=2 -coverprofile="$$coverage" ./...; \
	go tool cover -func="$$coverage"

test-integration:
	go test -race -count=2 -tags=integration ./...

test-mutation:
	go tool gremlins unleash . --workers 2 \
	  -E '(^|/)(cmd|config|container|generated|http|infra|model|repository)/' \
	  -E 'enums.enum.go' -S lc

fmt:
	golangci-lint fmt

lint:
	golangci-lint run ./...

generate:
	go tool oapi-codegen \
      -generate types,chi-server \
      -package api \
      -include-operation-ids createTrip,getTrip,finishTrip,health,ready \
      -o internal/generated/api.gen.go \
      contracts/openapi/trip-service.openapi.yaml
	go generate ./internal/...
