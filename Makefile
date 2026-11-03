.PHONY: migrate run test lint generate

-include .env
-include .env.local
export

migrate:
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

run:
	go run ./cmd/trip-service

test:
	go test -race ./...

lint:
	golangci-lint run ./...

generate:
	go tool oapi-codegen \
      -generate types,chi-server \
      -package api \
      -include-operation-ids createTrip,getTrip,finishTrip,health,ready \
      -o internal/generated/api.gen.go \
      contracts/openapi/trip-service.openapi.yaml
	go generate ./internal/domain/trip/
