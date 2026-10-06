# Guided tour: see everything in the browser

Start the stack: `cd ~/Documents/pocketcast && docker compose up -d`. Give it about 20 seconds.

Logins:
- Django admin: `admin` / `admin12345` (create it with `docker compose exec catalog python manage.py createsuperuser`)
- Demo listener: `demo` / `demo-password-1`

| # | Open | Login | What it shows |
|---|---|---|---|
| 1 | http://localhost:8000/admin/ | admin | Content team's view: shows, episodes, wallets, ledger, unlocks |
| 2 | http://localhost:8000/api-auth/login/?next=/api/v1/shows | demo | The REST API as clickable pages (what the mobile app calls) |
| 3 | http://localhost:8081/v1/trending | none | Go service: trending shows from Redis |
| 4 | http://localhost:8088 | none | Kafka UI (Redpanda Console): topics and every message |
| 5 | http://localhost:5540 | none | Redis UI (RedisInsight): live keys the Go service writes |
| 6 | http://localhost:8089 | see step 6 | Database UI (Adminer): the Postgres tables |
| 7 | http://localhost:9090 | none | Prometheus: metrics from the Go service |

## 1. Django admin: the catalog (Django)
- **Shows**: open "Insta Millionaire". The episodes are listed inline. Episodes 1–3 are free; 4–10 cost 25 coins. **Completions** is filled in by the Kafka consumer.
- **Wallets**: each user's balance. It's read-only, because balances only change through code that also writes the ledger.
- **Coin transactions**: the ledger. CREDIT for top-ups, DEBIT for unlocks, with `balance_after` and a unique `reference`.
- **Episode unlocks**: which user owns which paid episode.
- Try it: set a show to unpublished and it disappears from the API in step 2.

## 2. Browsable API: what the app sees (Django REST Framework)
After logging in as `demo`:
- `/api/v1/shows`: the list. Try `/api/v1/shows?genre=horror`.
- `/api/v1/shows/the-haunted-haveli`: demo has unlocked one paid episode, so it shows `locked: false` and has an `audio_url`. Other paid episodes show `locked: true` and `audio_url: null`.
- `/api/v1/wallet` and `/api/v1/wallet/transactions`: balance and history.
- `/api/v1/episodes/<id>/unlock`: the page has a **POST** button. Click it on a locked episode and the wallet goes down by 25. Click again and you get `already_unlocked: true` with no second charge.
- `/api/v1/wallet/top-up`: POST `{"amount": 50, "payment_ref": "my_ref_1"}`. POST the same thing again and nothing is credited twice.

## 3. Playback API (Go + Gin)
These need the `X-User-ID` header, so use the terminal. Demo's user id is shown by `/api/v1/auth/register` or the admin's Users page.
```bash
curl -s localhost:8081/v1/continue-listening -H "X-User-ID: 4"
curl -s -X POST localhost:8081/v1/progress -H "X-User-ID: 4" -d '{"episode_id":14,"position_seconds":580}'   # completed: true
curl -s localhost:8081/v1/trending
```

## 4. Kafka UI: events between the services
- **Topics → playback.episode_completed → Messages**: one JSON message per finished episode (`event_id`, `user_id`, `episode_id`, `show_id`). The key is the user id.
- **Consumer Groups → catalog-service**: lag should be 0, meaning the Django consumer has processed everything.
- Try it: finish an episode with the curl in step 3, refresh, and see the new message. Then check the episode's Completions in the admin; it went up by 1.

## 5. Redis UI: playback state
If no database is listed, click **Add Redis database**, host `redis`, port `6379`.
- `progress:4`: hash, episode to position in seconds (the resume point).
- `recent:4`: sorted set, episodes by last-played time ("continue listening").
- `completed:4:<episode>`: marker set once per finished episode, so the Kafka event is sent only once.
- `trending:<today>`: sorted set, show to completions today. The trending API adds up the last 7 days.

## 6. Database UI: Postgres tables
Log in with System **PostgreSQL**, Server `postgres`, Username `pocketcast`, Password `pocketcast`, Database `pocketcast`.
- Tables: `catalog_show`, `catalog_episode`, `wallet_wallet`, `wallet_cointransaction`, `wallet_episodeunlock`, `catalog_processedevent`.
- Click a table, then **Structure** to see the unique constraints and indexes that make the money logic safe.
- Use **SQL command** to run queries, for example:
  `select type, amount, balance_after, reference from wallet_cointransaction order by id desc limit 10;`

## 7. Prometheus: monitoring
Query box examples (press Execute, then the Graph tab):
- `playback_http_requests_total`: requests per route and status (the 403s are locked-episode attempts).
- `playback_episodes_completed_total`
- `rate(playback_http_requests_total[1m])`
- **Status → Targets**: `playback-service` should be UP.

## Run all checks
`python3 scripts/e2e_test.py` runs 17 checks, including the concurrency ones. Afterwards, look at the Kafka UI and the admin again to see the new data it created.

## Stop and start
- `docker compose stop` / `docker compose start`: data is kept.
- `docker compose down`: removes containers. Data is kept in the `pgdata` volume.
- `docker compose down -v`: wipes everything.
