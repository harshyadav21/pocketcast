# Study guide: own this project before the interview

Interviewers pick one project line on your resume and go deep. Work through this file until you can explain every flow below without looking, and change the code live.

## 1. Learn the basics first (about 1 week)

**Go (3–4 days)**
- [Tour of Go](https://go.dev/tour): basics, methods, interfaces, goroutines, channels.
- Read [Effective Go](https://go.dev/doc/effective_go) sections on errors, interfaces and concurrency.
- Things to know cold for this repo:
  - `error` values and `errors.Is`.
  - Structs with methods.
  - Small interfaces declared where they're used (`progress.AccessChecker`, `progress.Publisher`).
  - `context.Context` passed through every call.
  - `sync.Mutex`.
  - Goroutines and `sync.WaitGroup` (used in the concurrency test).
  - Table-driven tests (`api/handler_test.go`).
- Gin: `gin.Context`, route groups, middleware (`requireUser`, `observe`), and `ShouldBindJSON` with `binding` tags.

**Django (2–3 days)**
- Django tutorial parts 1–2 and 7 (models, admin), then the DRF tutorial parts 1–3.
- Things to know cold:
  - Models, migrations and `Meta.constraints`.
  - `select_related` vs `prefetch_related`.
  - `transaction.atomic` and `select_for_update`.
  - `F()` expressions.
  - DRF serializers, `APIView` vs generic views, authentication vs permission classes, and pagination.
  - Management commands.

**Exercise:** delete a function (for example `unlock_episode` or `markCompleted`) and rewrite it from memory until the tests pass.

## 2. Code flows (say these out loud)

**Unlock an episode**: `POST /api/v1/episodes/{id}/unlock`
1. `catalog/views.py UnlockEpisodeView.post` returns 404 if the episode is missing or unpublished.
2. `wallet/services.py unlock_episode` is `@transaction.atomic`.
3. It raises `EpisodeIsFree` (400) for a free episode.
4. `_lock_wallet` makes sure the wallet exists, then runs `SELECT ... FOR UPDATE`. Other unlocks for this user wait here.
5. If an unlock already exists, it returns it (200, no charge). This check happens *after* the lock, which is why it's safe.
6. If the balance is below the price, it raises `InsufficientCoins` (402).
7. It deducts the balance, then creates a `CoinTransaction` (DEBIT, `balance_after`, unique reference) and an `EpisodeUnlock`. The transaction commits and the lock is released.

**Progress heartbeat**: `POST /v1/progress`
1. `api/handler.go`: `requireUser` reads `X-User-ID`, `ShouldBindJSON` validates the body, and the handler calls `progress.Service.SaveProgress`.
2. `catalog.Client.CheckAccess` checks the in-memory cache, otherwise calls `GET /internal/v1/access`. Only "allowed" answers are cached.
3. Not allowed returns 403. A position outside `0..duration` returns 422.
4. `Store.SavePosition` runs MULTI/EXEC: HSET the position, ZADD to recent, trim to 50, set the TTL.
5. Below 95%, it returns `completed=false`.
6. At 95% or more, `markCompleted` runs SETNX on the completion marker. Only the first caller continues.
7. That caller publishes `episode_completed` to Kafka, keyed by user id. If the publish fails, it deletes the marker so the next heartbeat retries. If it succeeds, it runs ZINCRBY on `trending:{today}`.

**Consumer**: `python manage.py consume_playback_events`
1. Reads a Kafka message and calls `apply_episode_completed`.
2. In one transaction: `get_or_create(ProcessedEvent)`. If the event already exists, it skips it. Otherwise it updates `completions = F('completions') + 1`.
3. Commits the offset only after the DB commit. A crash before the commit means the message is redelivered, and the dedupe makes that harmless.

## 3. Likely interview questions, with answers

1. **Why `select_for_update` and not just a check in Python?**
   Two requests can both read balance=25 and both deduct. The row lock serializes them. The test shows 10 unlocks succeeding without it and 4 with it.
2. **Why not optimistic locking (a version column)?**
   That works too, but it needs retry loops. Contention on one user's wallet is low and the transaction is short, so a pessimistic lock is simpler and correct.
3. **What if two different users unlock at the same time?**
   They lock different wallet rows, so they don't block each other.
4. **How is top-up idempotent? What about the same `payment_ref` sent concurrently?**
   Both requests lock the same wallet first, so the second one sees the first one's row. Even without the lock, the unique index on `reference` would reject the second insert.
5. **Why Redis for progress instead of Postgres?**
   Heartbeats arrive every few seconds per active listener. That's write-heavy, and losing a few seconds of position is acceptable. Redis handles it cheaply. If durability mattered, I'd flush to a DB in batches.
6. **How do you guarantee one completion event per user and episode?**
   SETNX is atomic. There's a test with 20 goroutines sending heartbeats at the same moment, and only one event is published.
7. **What if Kafka is down?**
   The progress save still succeeds, the marker is cleared, and the next heartbeat retries. The `playback_event_publish_failures_total` metric shows it. The stronger fix is an outbox table.
8. **Kafka is at-least-once. How do you avoid counting twice?**
   With the `ProcessedEvent` dedupe in the same transaction, and by committing the offset after the DB commit.
9. **Why key Kafka messages by user id?**
   It keeps one user's events in order on one partition, and spreads load across partitions.
10. **How does trending expire old data?**
    There's one sorted set per day with an 8-day TTL. The endpoint runs ZUNION over the last 7 keys.
11. **How would you scale playback to 10x traffic?**
    The service is stateless, so add replicas behind a load balancer. Use Redis Cluster, since keys are per user and shard well. Add Kafka partitions. The access cache cuts calls to the catalog.
12. **Why does the access cache store only "allowed"?**
    Access is only ever granted, never revoked. A cached "no" would block a user right after they pay.
13. **How do services authenticate each other?**
    With a shared internal token compared using `hmac.compare_digest` (constant time). In production I'd use mTLS or signed service tokens.
14. **Why Go for playback and Django for catalog?**
    Playback is a hot path with a high request rate, where Go's low latency and goroutines fit. Catalog is CRUD plus admin plus transactional money logic, where Django's ORM, migrations and admin save a lot of time.

## 4. Live-change drills (practice these)
- Add a `GET /v1/progress/show/{showID}` that returns progress for every episode of a show.
- Add a daily free-coin bonus endpoint that can be claimed once per day. Hint: a unique reference like `bonus:{user}:{date}`.
- Make the completion threshold configurable through an environment variable.
- Add a `language` filter to trending.
