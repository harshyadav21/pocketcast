# PocketCast: project guide

Everything you need to understand, run and test this project. The README is the short version. This file is the long one.

## 1. What this project is

It's a **backend-only** system. There is no frontend. A real product would have a mobile app calling these APIs; here you play the app's role with curl or Postman. The Django admin (`/admin`) is the only built-in UI, and it's for content managers.

It models the core of an audio-series app:

| Feature | Where | Stored in |
|---|---|---|
| Sign up and log in (tokens) | catalog-service (Django) | PostgreSQL |
| Shows and episodes, free or paid | catalog-service | PostgreSQL |
| Coin wallet, top-ups, transaction history | catalog-service | PostgreSQL |
| Unlocking a paid episode with coins | catalog-service | PostgreSQL |
| Saving listening position and resume | playback-service (Go) | Redis |
| "Continue listening" list | playback-service | Redis |
| Detecting that an episode was finished | playback-service | Redis, then a Kafka event |
| Counting completions per episode | catalog-service Kafka consumer | PostgreSQL |
| Trending shows (last 7 days) | playback-service | Redis |

### Why two services in two languages?
- **Catalog** is CRUD plus admin plus money. Django gives you the ORM, migrations, an admin panel and transactions for free.
- **Playback** gets a request every few seconds from every active listener. Go handles many concurrent requests cheaply, and Redis absorbs the write load.
- Kafka decouples them. Playback announces "user X finished episode Y", and any service (the catalog here, recommendations or analytics later) can react without playback knowing about it.

## 2. What you should know to build or understand this

| Topic | What's used here | Where to learn |
|---|---|---|
| HTTP and REST | Methods, status codes (201/200/402/409/422), JSON, headers | MDN "HTTP overview" |
| Python and Django | Models, migrations, ORM queries, admin, management commands | Django official tutorial (parts 1–2, 7) |
| Django REST Framework | Serializers, APIView and generic views, token auth, pagination | DRF tutorial (parts 1–4) |
| Go | Structs, interfaces, errors, goroutines, `context`, tests | Tour of Go, Effective Go |
| Gin | Routes, groups, middleware, JSON binding | Gin docs "Quickstart" |
| SQL and PostgreSQL | Tables, unique constraints, indexes, transactions, `SELECT ... FOR UPDATE` | PostgreSQL docs, ch. 13 "Concurrency Control" |
| Redis | Hash, sorted set, `SETNX`, TTL, MULTI/EXEC | redis.io "Data types" |
| Kafka | Topics, partitions, keys, consumer groups, offsets, at-least-once delivery | Confluent "Kafka 101" |
| Docker | Images, containers, Dockerfile, Compose, ports, volumes | Docker "Get started" |
| Concurrency | Race conditions, locks, idempotency | Section 10 below and `notes/STUDY_GUIDE.md` |

## 3. What's in the repo

```
pocketcast/
├── catalog-service/                 Django project
│   ├── pocketcast/                  settings.py (env-based config), urls.py, auth_views.py (register)
│   ├── catalog/                     Show, Episode, ProcessedEvent models; API views; admin
│   │   ├── events.py                idempotent handler for episode_completed
│   │   └── management/commands/     consume_playback_events (Kafka consumer), seed_demo (demo data)
│   ├── wallet/                      Wallet, CoinTransaction, EpisodeUnlock models
│   │   └── services.py              top_up / unlock_episode: transactions + row locks
│   ├── */tests.py                   Django tests (including the concurrency test)
│   ├── requirements.txt, Dockerfile
├── playback-service/                Go service
│   ├── cmd/server/main.go           wiring + graceful shutdown
│   └── internal/
│       ├── api/                     Gin routes, middleware, error to status mapping
│       ├── progress/                service.go (business logic), store.go (Redis)
│       ├── catalog/                 HTTP client for the catalog with an access cache
│       ├── events/                  Kafka publisher
│       ├── metrics/, config/
├── scripts/
│   ├── e2e_test.py                  end-to-end tests against running services
│   └── create_local_db.sql          creates the DB and user on a local PostgreSQL
├── docs/schema.sql                  the SQL Django runs to create the tables (reference)
├── notes/STUDY_GUIDE.md             code walkthrough and interview Q&A
├── docker-compose.yml               Postgres, Redis, Redpanda, both services, consumer, Prometheus
├── Makefile                         shortcuts (make up, make test, make e2e, ...)
└── .github/workflows/ci.yml         CI: Go tests + Django tests on Postgres
```

### Database tables (created by `python manage.py migrate`)
| Table | Purpose | Key constraints |
|---|---|---|
| `catalog_show` | Shows | `slug` unique; index on (genre, language, is_published) |
| `catalog_episode` | Episodes, price, `completions` counter | unique (show, number) |
| `catalog_processedevent` | Kafka event ids already applied | `event_id` unique |
| `wallet_wallet` | One coin balance per user | `user_id` unique |
| `wallet_cointransaction` | Append-only ledger with `balance_after` | `reference` unique (idempotency) |
| `wallet_episodeunlock` | Which user owns which episode | unique (user, episode) |
| `auth_user`, `authtoken_token`, ... | Django's built-in users and tokens | |

You never write `CREATE TABLE` by hand. Django generates it from `models.py`, and `docs/schema.sql` shows the exact SQL.

### Redis keys (playback-service)
| Key | Type | Content |
|---|---|---|
| `progress:{user}` | hash | episodeID to position in seconds |
| `recent:{user}` | sorted set | episodeID scored by last-played time (max 50) |
| `completed:{user}:{episode}` | string | first-completion marker (`SETNX`) |
| `trending:{yyyy-mm-dd}` | sorted set | showID scored by completions that day (8-day TTL) |

## 4. Tools to install

| Tool | Needed for | macOS |
|---|---|---|
| Docker + Compose v2.23+ | The Docker way (section 5) | Docker Desktop, or `brew install colima docker docker-compose && colima start` |
| Python 3.12 | Running catalog locally | `brew install python@3.12` |
| Go 1.26+ | Running playback locally | `brew install go` |
| PostgreSQL 16+ | Running catalog locally against your own DB | postgresql.org installer or `brew install postgresql@16` |

## 5. Run everything with Docker

This is the easiest way, and the one anyone cloning from GitHub should use.

```bash
git clone https://github.com/harshyadav21/pocketcast.git
cd pocketcast
docker compose up -d --build
docker compose ps                     # all services should be "Up"
```

What happens:
1. Postgres, Redis and Redpanda (a Kafka-compatible broker) start.
2. `catalog` runs `migrate` (creates the tables) and `seed_demo` (3 shows × 10 episodes; episodes 1–3 free, 4–10 cost 25 coins), then starts Gunicorn on :8000.
3. `catalog-consumer` starts listening to the `playback.episode_completed` topic.
4. `playback` starts on :8081, and Prometheus starts on :9090.

Useful commands:
```bash
docker compose logs -f playback catalog-consumer   # follow logs
docker compose exec catalog python manage.py createsuperuser   # admin login for /admin
docker compose exec postgres psql -U pocketcast    # SQL shell
docker compose exec redis redis-cli                # Redis shell
docker compose down                                # stop (data kept)
docker compose down -v                             # stop and delete data
docker compose up -d --build catalog               # rebuild one service after a code change
```

Port map: catalog 8000, playback 8081, Prometheus 9090, Postgres **5433** on the host (so it doesn't clash with a local Postgres on 5432), Redis 6379, Kafka 19092 (host) / 9092 (inside Docker).

## 6. Run locally (without Docker for the apps)

Use this while developing. The Django and Go processes run on your machine against your **locally installed PostgreSQL**. Redis and Kafka still come from Docker, because they're one command there.

```bash
# 0. Stop the Docker app containers if they're running (they use the same ports)
docker compose stop catalog catalog-consumer playback

# 1. One-time: create the database and user in your local Postgres (asks for the postgres superuser password)
psql -h localhost -U postgres -f scripts/create_local_db.sql
#    EDB installer path on macOS: /Library/PostgreSQL/18/bin/psql

# 2. One-time: Python virtualenv
make venv

# 3. Redis + Kafka
make local-infra

# 4. Create the tables + demo data in local Postgres
make migrate                       # uses localhost:5432; for another port: make migrate POSTGRES_PORT=5433

# 5. Start the three processes, each in its own terminal
make run-catalog                   # Django dev server  -> http://localhost:8000
make run-consumer                  # Kafka consumer
make run-playback                  # Go service         -> http://localhost:8081

# 6. Test
python3 scripts/e2e_test.py
```

Check the tables were created:
```bash
psql -h localhost -U pocketcast -d pocketcast -c '\dt'          # password: pocketcast
```

### Fully without Docker (Homebrew Redis + Kafka)
Not tested on this machine; the Docker route for Redis and Kafka above is the tested one.
```bash
brew install redis kafka
brew services start redis          # localhost:6379
brew services start kafka          # localhost:9092 (KRaft mode, no ZooKeeper)
make migrate
make run-catalog                   # terminal 1
make run-consumer KAFKA=localhost:9092   # terminal 2
make run-playback KAFKA=localhost:9092   # terminal 3
# stop later: brew services stop kafka redis
```

### Python environment: venv vs pyenv
- **venv** (used here): an isolated folder (`catalog-service/.venv`) holding this project's packages. `make venv` creates it with `python3.12`. Run things through `.venv/bin/python`, or `source .venv/bin/activate` first.
- **pyenv** is only for installing and switching between Python *versions*. You don't need it if `python3.12 --version` works (`brew install python@3.12`). macOS's built-in `python3` is 3.9, which is too old for Django 5.2.

Configuration is all environment variables (see `catalog-service/pocketcast/settings.py` and `playback-service/internal/config/config.go`): `POSTGRES_HOST/PORT/DB/USER/PASSWORD`, `REDIS_ADDR`, `KAFKA_BROKERS` / `KAFKA_BOOTSTRAP_SERVERS`, `CATALOG_URL`, `INTERNAL_API_TOKEN`. Without `POSTGRES_HOST`, Django falls back to SQLite for a quick start.

## 7. Using the API (walkthrough)

### In a browser
| Open | What you see |
|---|---|
| http://localhost:8000/api-auth/login/ | Log in with a registered user. Then the API pages below render as clickable HTML (DRF browsable API) |
| http://localhost:8000/api/v1/shows | Show list; click through to `/api/v1/shows/insta-millionaire`. The wallet and unlock pages have forms for POST |
| http://localhost:8000/admin/ | Django admin: edit shows and episodes, view wallets and ledger (needs `createsuperuser`) |
| http://localhost:8081/v1/trending | Trending JSON (no login) |
| http://localhost:8081/metrics | Raw Prometheus metrics |
| http://localhost:9090 | Prometheus UI (Docker only) |

Playback endpoints other than trending need an `X-User-ID` header, which a browser can't add. Use curl, or Postman / Insomnia / Bruno.

### With curl

```bash
# Register and save the token + user id
curl -s -X POST localhost:8000/api/v1/auth/register \
  -H 'Content-Type: application/json' -d '{"username":"asha","password":"long-password-1"}'
# -> {"user_id": 1, "token": "abc..."}
TOKEN=abc...; USER_ID=1
AUTH="Authorization: Token $TOKEN"

# Browse
curl -s localhost:8000/api/v1/shows -H "$AUTH"
curl -s "localhost:8000/api/v1/shows?genre=horror" -H "$AUTH"
curl -s localhost:8000/api/v1/shows/insta-millionaire -H "$AUTH"      # episode 4+ show "locked": true

# Buy coins, then unlock episode 4
curl -s -X POST localhost:8000/api/v1/wallet/top-up -H "$AUTH" \
  -H 'Content-Type: application/json' -d '{"amount":100,"payment_ref":"pay_001"}'
curl -s -X POST localhost:8000/api/v1/episodes/4/unlock -H "$AUTH"
curl -s localhost:8000/api/v1/wallet -H "$AUTH"                       # balance 75
curl -s localhost:8000/api/v1/wallet/transactions -H "$AUTH"

# Listen: the app would send these every few seconds
curl -s -X POST localhost:8081/v1/progress -H "X-User-ID: $USER_ID" -d '{"episode_id":4,"position_seconds":120}'
curl -s -X POST localhost:8081/v1/progress -H "X-User-ID: $USER_ID" -d '{"episode_id":4,"position_seconds":590}'  # -> completed: true
curl -s localhost:8081/v1/progress/4 -H "X-User-ID: $USER_ID"
curl -s localhost:8081/v1/continue-listening -H "X-User-ID: $USER_ID"
curl -s localhost:8081/v1/trending

# The Kafka consumer bumped the counter
curl -s localhost:8000/api/v1/episodes/4 -H "$AUTH"                   # "completions": 1
```

## 8. What to test (checklist)

| # | Scenario | Expected |
|---|---|---|
| 1 | List shows without a token | 401 |
| 2 | Show detail before unlocking | paid episodes `locked: true`, `audio_url: null` |
| 3 | Progress on a locked episode | 403 from playback |
| 4 | Unlock with 0 coins | 402, balance unchanged |
| 5 | Top-up, then the same `payment_ref` again | 201, then 200; balance credited once |
| 6 | Same `payment_ref` with a different amount | 409 |
| 7 | Unlock the same episode twice | 201, then 200 with `already_unlocked: true`, charged once |
| 8 | Unlock a free episode | 400 |
| 9 | Progress past the episode length | 422 |
| 10 | Heartbeats at 120 / 590 / 600 of 600 s | `completed` false / true / true |
| 11 | Completion leads to a Kafka event and the consumer | `completions` +1 on the episode |
| 12 | 10 parallel unlocks, money for 4 | exactly 4 succeed, balance 0 |
| 13 | 10 parallel unlocks of one episode | charged exactly once |
| 14 | 20 parallel "finished" heartbeats | exactly 1 Kafka event |
| 15 | Replay an old Kafka event | ignored (`applied=False`) |

`python3 scripts/e2e_test.py` automates 1–14. Number 15 is in section 9.

Automated tests:
```bash
make test     # Go: go test -race ./... | Django: manage.py test
# Run Django tests on Postgres so the row-lock concurrency test runs (it's skipped on SQLite):
cd catalog-service && POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=pocketcast POSTGRES_PASSWORD=pocketcast .venv/bin/python manage.py test
```

## 9. Testing Kafka by hand

Redpanda ships `rpk`, a Kafka CLI:

```bash
# Topics and messages
docker compose exec redpanda rpk topic list
docker compose exec redpanda rpk topic consume playback.episode_completed -f '%k %v\n'        # Ctrl+C to stop

# Consumer group: is the catalog consumer keeping up? (TOTAL-LAG should be 0)
docker compose exec redpanda rpk group describe catalog-service

# Watch the consumer apply events
docker compose logs -f catalog-consumer
```

**Prove the consumer is idempotent.** Re-send an event that was already processed. The counter must not change.
```bash
EV=$(docker compose exec -T redpanda rpk topic consume playback.episode_completed -n 1 -f '%v')
echo "$EV" | docker compose exec -T redpanda rpk topic produce playback.episode_completed -k replay
docker compose logs catalog-consumer --tail 1        # -> "event ... applied=False"
```

**Kafka down?** Run `docker compose stop redpanda`, finish an episode (progress past 95%), and look at `curl -s localhost:8081/metrics | grep publish_failures`. The counter goes up and nothing is lost. Run `docker compose start redpanda` and send one more heartbeat for that episode. The event is published then.

**Metrics:** open http://localhost:9090 and query `playback_http_requests_total`, `playback_episodes_completed_total` or `playback_event_publish_failures_total`.

## 10. The tricky parts, explained

- **Double spending.** Two unlock requests both read balance=25 and both deduct, leaving -25. Fix: `select_for_update()` locks the wallet row inside `transaction.atomic`, so the second request waits until the first commits, then sees the new balance. The test proves it: without the lock, 10 of 10 unlocks succeed on 100 coins; with it, exactly 4 do.
- **Retries (idempotency).** Payment callbacks and mobile clients retry. A unique `reference` on the ledger means a retry finds the existing row instead of crediting twice.
- **Exactly-once events from many heartbeats.** `SETNX` is atomic in Redis: of 20 simultaneous requests, exactly one gets `true`.
- **At-least-once Kafka.** A consumer can crash after updating the DB but before committing the offset, and then the message comes again. Storing `event_id` in a unique table in the same transaction makes the second delivery a no-op.

More detail and interview questions: `notes/STUDY_GUIDE.md`.

## 11. Troubleshooting

| Problem | Fix |
|---|---|
| `port is already allocated` on 5432 | A local Postgres uses 5432. Compose already maps to **5433**. |
| `password authentication failed` on localhost:5432 | You're hitting your local Postgres, not Docker's. Use port 5433 for Docker, or run `scripts/create_local_db.sql` on the local one. |
| Colima: `mkdir /Users/...: operation not permitted` | macOS blocks the VM from reading `~/Documents`. This compose file uses no bind mounts; if you add one, move the repo outside `~/Documents` or grant access. |
| `Cannot connect to the Docker daemon` | Start Docker Desktop or run `colima start`. |
| Playback 500s right after startup | Catalog wasn't ready yet. Retry after a few seconds (`docker compose logs catalog`). |
| E2E fails on "list shows" | No demo data. Run `make migrate`, or `docker compose exec catalog python manage.py seed_demo`. |

## 12. What a good project README usually contains

The README is this repo's front page on GitHub. Most good ones include:
1. **Name and a one-line description**, so a reader knows what it is in 5 seconds.
2. **Features**: what it does, in bullets.
3. **Architecture diagram**: boxes and arrows (Mermaid renders on GitHub).
4. **Tech stack**.
5. **Quick start**: the fewest commands to get it running (usually Docker).
6. **Configuration**: environment variables.
7. **API reference** or a link to one (OpenAPI or Postman).
8. **How to test.**
9. **Project structure.**
10. **Design decisions and trade-offs**: what interviewers and senior engineers read first.
11. **Roadmap / known limitations.**
12. **License** (and contributing guidelines for open source).

Badges (CI status), screenshots or a GIF (for UI projects), and a deployed demo link are nice extras. Longer material like this guide goes in separate files and gets linked from the README.
