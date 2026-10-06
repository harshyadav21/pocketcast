import threading
import unittest

from django.contrib.auth.models import User
from django.db import connection, connections
from django.test import TestCase, TransactionTestCase
from rest_framework.test import APIClient

from catalog.models import Episode, Show
from wallet import services
from wallet.exceptions import DuplicatePaymentReference, EpisodeIsFree, InsufficientCoins
from wallet.models import CoinTransaction, EpisodeUnlock


def make_episodes():
    show = Show.objects.create(title="Saving Nora", slug="saving-nora", genre="ROMANCE", is_published=True)
    free = Episode.objects.create(show=show, number=1, title="Ep 1", duration_seconds=600,
                                  audio_url="https://cdn.example.com/1.mp3", is_free=True)
    paid = Episode.objects.create(show=show, number=2, title="Ep 2", duration_seconds=600,
                                  audio_url="https://cdn.example.com/2.mp3", coin_price=25)
    return free, paid


class WalletServiceTests(TestCase):
    def setUp(self):
        self.user = User.objects.create_user("listener", password="secret-pass-1")
        self.free, self.paid = make_episodes()

    def test_topUp_sameReferenceTwice_creditsOnce(self):
        _, first = services.top_up(self.user, 100, "pay-1")
        _, second = services.top_up(self.user, 100, "pay-1")

        self.assertTrue(first)
        self.assertFalse(second)
        self.assertEqual(services.get_wallet(self.user).balance, 100)

    def test_topUp_sameReferenceDifferentAmount_raisesConflict(self):
        services.top_up(self.user, 100, "pay-1")
        with self.assertRaises(DuplicatePaymentReference):
            services.top_up(self.user, 500, "pay-1")

    def test_unlockEpisode_enoughCoins_debitsAndRecordsLedger(self):
        services.top_up(self.user, 100, "pay-1")

        unlock, created = services.unlock_episode(self.user, self.paid.id)

        self.assertTrue(created)
        self.assertEqual(services.get_wallet(self.user).balance, 75)
        self.assertEqual(unlock.transaction.balance_after, 75)
        self.assertEqual(unlock.transaction.type, CoinTransaction.Type.DEBIT)

    def test_unlockEpisode_alreadyUnlocked_doesNotChargeAgain(self):
        services.top_up(self.user, 100, "pay-1")
        services.unlock_episode(self.user, self.paid.id)

        _, created = services.unlock_episode(self.user, self.paid.id)

        self.assertFalse(created)
        self.assertEqual(services.get_wallet(self.user).balance, 75)

    def test_unlockEpisode_notEnoughCoins_raisesAndLeavesBalance(self):
        services.top_up(self.user, 10, "pay-1")

        with self.assertRaises(InsufficientCoins):
            services.unlock_episode(self.user, self.paid.id)
        self.assertEqual(services.get_wallet(self.user).balance, 10)
        self.assertFalse(EpisodeUnlock.objects.exists())

    def test_unlockEpisode_exactBalance_succeedsWithZeroLeft(self):
        services.top_up(self.user, 25, "pay-1")
        services.unlock_episode(self.user, self.paid.id)
        self.assertEqual(services.get_wallet(self.user).balance, 0)

    def test_unlockEpisode_freeEpisode_raises(self):
        with self.assertRaises(EpisodeIsFree):
            services.unlock_episode(self.user, self.free.id)


class WalletApiTests(TestCase):
    def setUp(self):
        self.user = User.objects.create_user("listener", password="secret-pass-1")
        self.client = APIClient()
        self.client.force_authenticate(self.user)
        _, self.paid = make_episodes()

    def test_unlockApi_insufficientCoins_returns402(self):
        response = self.client.post(f"/api/v1/episodes/{self.paid.id}/unlock")
        self.assertEqual(response.status_code, 402)

    def test_unlockApi_retry_returns200WithoutCharging(self):
        self.client.post("/api/v1/wallet/top-up", {"amount": 50, "payment_ref": "pay-1"})

        first = self.client.post(f"/api/v1/episodes/{self.paid.id}/unlock")
        retry = self.client.post(f"/api/v1/episodes/{self.paid.id}/unlock")

        self.assertEqual(first.status_code, 201)
        self.assertEqual(retry.status_code, 200)
        self.assertTrue(retry.data["already_unlocked"])
        self.assertEqual(retry.data["balance"], 25)

    def test_topUpApi_zeroAmount_returns400(self):
        response = self.client.post("/api/v1/wallet/top-up", {"amount": 0, "payment_ref": "pay-1"})
        self.assertEqual(response.status_code, 400)

    def test_register_createsWalletAndToken(self):
        response = APIClient().post("/api/v1/auth/register", {"username": "new-user", "password": "long-password-1"})

        self.assertEqual(response.status_code, 201)
        self.assertIn("token", response.data)
        self.assertEqual(User.objects.get(username="new-user").wallet.balance, 0)


@unittest.skipIf(connection.vendor != "postgresql", "row locking needs Postgres")
class UnlockConcurrencyTests(TransactionTestCase):
    def test_unlockEpisode_concurrentDifferentEpisodes_neverOverspends(self):
        user = User.objects.create_user("listener", password="secret-pass-1")
        show = Show.objects.create(title="Race", slug="race", genre="DRAMA", is_published=True)
        episodes = [
            Episode.objects.create(show=show, number=n, title=f"Ep {n}", duration_seconds=600,
                                   audio_url="https://cdn.example.com/x.mp3", coin_price=25)
            for n in range(1, 11)
        ]
        services.top_up(user, 100, "pay-1")  # enough for exactly 4 of the 10 unlocks

        start = threading.Barrier(len(episodes))
        results = []

        def unlock(episode):
            start.wait()
            try:
                services.unlock_episode(user, episode.id)
                results.append("ok")
            except InsufficientCoins:
                results.append("insufficient")
            finally:
                connections.close_all()

        threads = [threading.Thread(target=unlock, args=(ep,)) for ep in episodes]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        self.assertEqual(results.count("ok"), 4)
        self.assertEqual(services.get_wallet(user).balance, 0)
        self.assertEqual(EpisodeUnlock.objects.filter(user=user).count(), 4)
