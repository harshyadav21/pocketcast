#!/usr/bin/env python3
"""End-to-end test against running services (docker compose or local).

    python3 scripts/e2e_test.py
    python3 scripts/e2e_test.py --catalog http://localhost:8000 --playback http://localhost:8081

Needs the seed data (python manage.py seed_demo). Uses only the standard library.
"""
import argparse
import json
import sys
import time
import urllib.error
import urllib.request
import uuid
from concurrent.futures import ThreadPoolExecutor

failures = 0


def call(method, url, body=None, headers=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json", **(headers or {})})
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read() or b"null")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"null")


def check(name, condition, detail=""):
    global failures
    print(("PASS  " if condition else "FAIL  ") + name + ("" if condition else f"  -> {detail}"))
    if not condition:
        failures += 1


class Client:
    def __init__(self, catalog, playback):
        self.catalog, self.playback = catalog, playback
        status, body = call("POST", f"{catalog}/api/v1/auth/register",
                            {"username": f"e2e-{uuid.uuid4().hex[:8]}", "password": "long-password-1"})
        assert status == 201, f"register failed: {status} {body}"
        self.user_id, self.auth = body["user_id"], {"Authorization": f"Token {body['token']}"}

    def api(self, method, path, body=None):
        return call(method, f"{self.catalog}/api/v1{path}", body, self.auth)

    def progress(self, episode_id, position):
        return call("POST", f"{self.playback}/v1/progress",
                    {"episode_id": episode_id, "position_seconds": position}, {"X-User-ID": str(self.user_id)})

    def top_up(self, amount, ref=None):
        return self.api("POST", "/wallet/top-up", {"amount": amount, "payment_ref": ref or uuid.uuid4().hex})

    def balance(self):
        return self.api("GET", "/wallet")[1]["balance"]


def paid_episodes(client, slugs):
    episodes = []
    for slug in slugs:
        _, show = client.api("GET", f"/shows/{slug}")
        episodes += [dict(e, show_id=show["id"]) for e in show["episodes"] if not e["is_free"]]
    return episodes


def completions(client, episode_id):
    return client.api("GET", f"/episodes/{episode_id}")[1]["completions"]


def wait_for_completions(client, episode_id, expected, timeout=20):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if completions(client, episode_id) >= expected:
            break
        time.sleep(0.5)
    time.sleep(1)  # give a wrong duplicate a chance to show up too
    return completions(client, episode_id)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--catalog", default="http://localhost:8000")
    parser.add_argument("--playback", default="http://localhost:8081")
    args = parser.parse_args()

    print("== catalog + wallet ==")
    alice = Client(args.catalog, args.playback)
    status, shows = alice.api("GET", "/shows")
    check("list shows", status == 200 and shows["count"] >= 3, shows)
    episodes = paid_episodes(alice, ["insta-millionaire", "the-haunted-haveli"])
    ep = episodes[0]
    check("paid episode is locked and hides audio_url", ep["locked"] and ep["audio_url"] is None, ep)

    status, _ = alice.progress(ep["id"], 10)
    check("playback rejects a locked episode (403)", status == 403, status)

    status, _ = alice.api("POST", f"/episodes/{ep['id']}/unlock")
    check("unlock with no coins (402)", status == 402, status)

    ref = uuid.uuid4().hex
    s1, _ = alice.top_up(100, ref)
    s2, _ = alice.top_up(100, ref)
    s3, _ = alice.top_up(500, ref)
    check("top-up 201, retry 200, same ref + other amount 409", (s1, s2, s3) == (201, 200, 409), (s1, s2, s3))
    check("balance credited once", alice.balance() == 100, alice.balance())

    print("== concurrency over HTTP ==")
    # 10 different paid episodes in parallel; 100 coins buys exactly 4 at 25 each.
    targets = episodes[:10]
    with ThreadPoolExecutor(max_workers=10) as pool:
        codes = list(pool.map(lambda e: alice.api("POST", f"/episodes/{e['id']}/unlock")[0], targets))
    check("10 parallel unlocks: exactly 4 succeed", codes.count(201) == 4 and codes.count(402) == 6, sorted(codes))
    check("balance is 0, never negative", alice.balance() == 0, alice.balance())
    _, txns = alice.api("GET", "/wallet/transactions")
    check("ledger: 1 credit + 4 debits", txns["count"] == 5, txns["count"])

    bob = Client(args.catalog, args.playback)
    bob.top_up(50)
    with ThreadPoolExecutor(max_workers=10) as pool:
        codes = list(pool.map(lambda _: bob.api("POST", f"/episodes/{ep['id']}/unlock")[0], range(10)))
    check("same episode unlocked 10x in parallel: charged once", codes.count(201) == 1 and codes.count(200) == 9, sorted(codes))
    check("bob paid 25 once", bob.balance() == 25, bob.balance())

    print("== playback + kafka ==")
    owned = next(e for e in targets if alice.api("GET", f"/episodes/{e['id']}")[1]["locked"] is False)
    before = completions(alice, owned["id"])
    results = [alice.progress(owned["id"], pos)[1] for pos in (120, 400, 590, 600)]
    check("heartbeats: completed only from 95%", [r["completed"] for r in results] == [False, False, True, True], results)

    status, body = call("GET", f"{args.playback}/v1/progress/{owned['id']}", headers={"X-User-ID": str(alice.user_id)})
    check("resume position saved", status == 200 and body["position_seconds"] == 600, body)
    status, body = call("GET", f"{args.playback}/v1/continue-listening", headers={"X-User-ID": str(alice.user_id)})
    check("continue-listening lists the episode", body["items"][0]["episode_id"] == owned["id"], body)

    after = wait_for_completions(alice, owned["id"], before + 1)
    check("kafka event consumed: completions +1 in Postgres", after == before + 1, (before, after))

    # 20 simultaneous "finished" heartbeats from bob must still produce one event.
    before = completions(bob, ep["id"])
    with ThreadPoolExecutor(max_workers=20) as pool:
        list(pool.map(lambda _: bob.progress(ep["id"], 600), range(20)))
    after = wait_for_completions(bob, ep["id"], before + 1)
    check("20 parallel completion heartbeats -> exactly 1 event", after == before + 1, (before, after))

    status, body = call("GET", f"{args.playback}/v1/trending")
    show_ids = [s["show_id"] for s in body["items"]]
    check("trending includes the show", owned["show_id"] in show_ids, body)

    print(f"\n{'ALL PASSED' if failures == 0 else f'{failures} FAILED'}")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
