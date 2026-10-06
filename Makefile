# Docker: everything in containers
up:            ## build and start the whole stack
	docker compose up -d --build
down:          ## stop the stack (add -v to also wipe data: docker compose down -v)
	docker compose down
logs:
	docker compose logs -f catalog catalog-consumer playback

# Tests
test:          ## unit tests for both services
	cd playback-service && go test -race ./...
	cd catalog-service && .venv/bin/python manage.py test
e2e:           ## end-to-end test against running services
	python3 scripts/e2e_test.py

# Local: services run on your machine, Redis + Kafka in Docker, Postgres installed locally
POSTGRES_PORT ?= 5432
KAFKA ?= localhost:19092        # Redpanda in Docker; use KAFKA=localhost:9092 for a Homebrew Kafka
PYTHON ?= python3.12
CATALOG_ENV = POSTGRES_HOST=localhost POSTGRES_PORT=$(POSTGRES_PORT) KAFKA_BOOTSTRAP_SERVERS=$(KAFKA)

venv:          ## create the Python virtualenv for catalog-service
	cd catalog-service && $(PYTHON) -m venv .venv && .venv/bin/pip install -r requirements.txt
local-infra:   ## start only Redis and Redpanda (Kafka) in Docker
	docker compose up -d redis redpanda
migrate:       ## create tables in Postgres and load demo shows
	cd catalog-service && $(CATALOG_ENV) .venv/bin/python manage.py migrate && $(CATALOG_ENV) .venv/bin/python manage.py seed_demo
run-catalog:
	cd catalog-service && $(CATALOG_ENV) .venv/bin/python manage.py runserver 8000
run-consumer:
	cd catalog-service && $(CATALOG_ENV) .venv/bin/python manage.py consume_playback_events
run-playback:
	cd playback-service && KAFKA_BROKERS=$(KAFKA) go run ./cmd/server

.PHONY: up down logs test e2e venv local-infra migrate run-catalog run-consumer run-playback
