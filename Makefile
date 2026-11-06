include .env.example
-include .env
export

BIN          := bin/trip-service
IMAGE        := trip-service:local
OAPI_CONFIG  := internal/generated/oapi-codegen.yaml
OPENAPI_SPEC := contracts/openapi/trip-service.openapi.yaml
COMPOSE      := docker compose -f deploy/docker-compose.yaml

DOCKER_DATABASE_URL = $(subst @127.0.0.1:,@host.docker.internal:,$(subst @localhost:,@host.docker.internal:,$(DATABASE_URL)))
DOCKER_RUN = docker run --rm --add-host=host.docker.internal:host-gateway \
	--env-file .env.example -e DATABASE_URL="$(DOCKER_DATABASE_URL)"

.DEFAULT_GOAL := build

.PHONY: generate build run migrate migrate-down migrate-reset migrate-status test \
	docker-build docker-migrate docker-run compose-up compose-down

generate:
	go tool oapi-codegen -config $(OAPI_CONFIG) $(OPENAPI_SPEC)

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
	go test -race -count=1 ./...

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
