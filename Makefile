# Path to a sibling clone of nabhold/infrastructure, used by the
# *-infra targets below. Override with `INFRASTRUCTURE_DIR=/path make dev-up-infra`
# if it isn't cloned next to this repo.
INFRASTRUCTURE_DIR ?= ../infrastructure
INFRA_COMPOSE := $(INFRASTRUCTURE_DIR)/compose/compose.yaml
INFRA_ENV := $(INFRASTRUCTURE_DIR)/compose/.env

.PHONY: build test test-integration lint run sync-shared-contracts migrate migrate-up dev-up dev-down dev-logs dev-up-infra dev-down-infra dev-logs-infra dev-env-infra
build:
	go build ./cmd/controlplane
test:
	go test -race ./...
test-integration:
	TEST_DATABASE_URL="$${TEST_DATABASE_URL:-postgres://baobab_control:baobab_control@localhost:5432/baobab_control?sslmode=disable}" \
	SHARED_CONTRACTS_DIR="$${SHARED_CONTRACTS_DIR:-../shared}" \
	go test -race ./...
lint:
	go vet ./...
# Refresh internal/contracts/shared from a baobab-platform/shared checkout at
# the commit contracts.lock.yaml pins (TestEmbeddedContractsMatchShared
# fails until they match).
sync-shared-contracts:
	@test -n "$(SHARED_CONTRACTS_DIR)" || { echo "error: set SHARED_CONTRACTS_DIR to a baobab-platform/shared checkout" >&2; exit 1; }
	cd internal/contracts/shared && for f in $$(find . -type f -name '*.json'); do cp "$(SHARED_CONTRACTS_DIR)/contracts/$$f" "$$f"; done
run:
	go run ./cmd/controlplane
migrate:
	go run ./cmd/migrate
migrate-up:
	migrate -path internal/store/postgres/migrations -database "$(DATABASE_URL)" up
dev-up:
	docker compose up -d --wait
dev-down:
	docker compose down
dev-logs:
	docker compose logs -f

# Run against nabhold/infrastructure's real Postgres+RabbitMQ instead of
# this repo's standalone stand-in - the same topology this repo runs
# against in shared/staging environments. See README's "Local topology
# options" before using these.
dev-up-infra:
	@test -f $(INFRA_ENV) || { echo "error: $(INFRA_ENV) not found - clone nabhold/infrastructure as a sibling directory (or set INFRASTRUCTURE_DIR) and follow its README to create compose/.env" >&2; exit 1; }
	docker compose --project-directory $(INFRASTRUCTURE_DIR)/compose -f $(INFRA_COMPOSE) --env-file $(INFRA_ENV) up -d --wait postgresql rabbitmq
dev-down-infra:
	docker compose --project-directory $(INFRASTRUCTURE_DIR)/compose -f $(INFRA_COMPOSE) --env-file $(INFRA_ENV) down
dev-logs-infra:
	docker compose --project-directory $(INFRASTRUCTURE_DIR)/compose -f $(INFRA_COMPOSE) --env-file $(INFRA_ENV) logs -f postgresql rabbitmq
# Prints the DATABASE_URL/RABBITMQ_URL to paste into .env for dev-up-infra,
# derived from nabhold/infrastructure's own compose/.env rather than
# duplicating its secrets here.
dev-env-infra:
	@test -f $(INFRA_ENV) || { echo "error: $(INFRA_ENV) not found - see dev-up-infra" >&2; exit 1; }
	@set -a; . $(INFRA_ENV); set +a; \
	echo "DATABASE_URL=postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@localhost:$${POSTGRES_PORT:-5432}/$${POSTGRES_DB}?sslmode=disable"; \
	echo "RABBITMQ_URL=amqp://$${RABBITMQ_DEFAULT_USER}:$${RABBITMQ_DEFAULT_PASS}@localhost:$${RABBITMQ_AMQP_PORT:-5672}/$${RABBITMQ_DEFAULT_VHOST:-nabhold}"
