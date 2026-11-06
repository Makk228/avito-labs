include .env.example
-include .env
export

BIN          := bin/trip-service
TEST_COUNT   ?= 1
IMAGE        := trip-service:local
OAPI_CONFIG  := internal/generated/oapi-codegen.yaml
OPENAPI_SPEC := contracts/openapi/trip-service.openapi.yaml
COMPOSE      := docker compose -f deploy/docker-compose.yaml

DOCKER_DATABASE_URL = $(subst @127.0.0.1:,@host.docker.internal:,$(subst @localhost:,@host.docker.internal:,$(DATABASE_URL)))
DOCKER_OTLP_ENDPOINT = $(subst //127.0.0.1:,//host.docker.internal:,$(subst //localhost:,//host.docker.internal:,$(OTEL_EXPORTER_OTLP_ENDPOINT)))
DOCKER_RUN = docker run --rm --add-host=host.docker.internal:host-gateway \
	--env-file .env.example -e DATABASE_URL="$(DOCKER_DATABASE_URL)" \
	-e OTEL_EXPORTER_OTLP_ENDPOINT="$(DOCKER_OTLP_ENDPOINT)" \
	-e OTEL_EXPORTER_OTLP_PROTOCOL="$(OTEL_EXPORTER_OTLP_PROTOCOL)" \
	-e OTEL_TRACES_SAMPLER="$(OTEL_TRACES_SAMPLER)" \
	-e OTEL_TRACES_SAMPLER_ARG="$(OTEL_TRACES_SAMPLER_ARG)" \
	-e OTEL_RESOURCE_ATTRIBUTES="$(OTEL_RESOURCE_ATTRIBUTES)" \
	-e LOG_LEVEL="$(LOG_LEVEL)" -e SERVICE_VERSION="$(SERVICE_VERSION)"

.DEFAULT_GOAL := build

.PHONY: generate build run migrate migrate-down migrate-reset migrate-status test \
	docker-build docker-migrate docker-run compose-up compose-down

generate:
	go tool oapi-codegen -config $(OAPI_CONFIG) $(OPENAPI_SPEC)
	go generate ./internal/...

build:
	go build -o $(BIN) ./cmd/trip-service

run: build
	./$(BIN)

migrate: build
	./$(BIN) migrate up

migrate-down: build
	./$(BIN) migrate down

migrate-reset: build
	./$(BIN) migrate reset

migrate-status: build
	./$(BIN) migrate status

test:
	go test -race -count=$(TEST_COUNT) -coverprofile=coverage.out ./...
	go test -race -count=$(TEST_COUNT) -coverprofile=coverage-business.out ./internal/domain/... ./internal/usecase/...
	go tool cover -func=coverage-business.out

.PHONY: lint fmt test-integration
lint:
	golangci-lint run

fmt:
	golangci-lint fmt

test-integration:
	go test -race -tags=integration -count=2 ./...

docker-build:
	docker build -f deploy/Dockerfile -t $(IMAGE) .

docker-migrate: docker-build
	$(DOCKER_RUN) $(IMAGE) migrate up

docker-run: docker-build
	$(DOCKER_RUN) --name trip-service -p 8080:8080 $(IMAGE)

compose-up:
	$(COMPOSE) up -d --build

compose-down:
	$(COMPOSE) down -v
