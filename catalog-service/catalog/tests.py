from django.contrib.auth.models import User
from django.test import TestCase, override_settings
from rest_framework.test import APIClient

from catalog.events import apply_episode_completed
from catalog.models import Episode, Show
from wallet import services as wallet_services


def make_show(title="Insta Millionaire", genre="DRAMA", published=True):
    show = Show.objects.create(title=title, slug=title.lower().replace(" ", "-"), genre=genre, is_published=published)
    Episode.objects.create(show=show, number=1, title="Ep 1", duration_seconds=600,
                           audio_url="https://cdn.example.com/1.mp3", is_free=True)
    Episode.objects.create(show=show, number=2, title="Ep 2", duration_seconds=600,
                           audio_url="https://cdn.example.com/2.mp3", coin_price=25)
    return show


class CatalogApiTests(TestCase):
    def setUp(self):
        self.user = User.objects.create_user("listener", password="secret-pass-1")
        self.client = APIClient()
        self.client.force_authenticate(self.user)
        self.show = make_show()

    def test_listShows_filterByGenre_returnsOnlyPublishedMatches(self):
        make_show("Haunted Haveli", genre="HORROR")
        make_show("Draft Show", genre="HORROR", published=False)

        response = self.client.get("/api/v1/shows", {"genre": "horror"})

        self.assertEqual(response.status_code, 200)
        self.assertEqual([s["slug"] for s in response.data["results"]], ["haunted-haveli"])

    def test_showDetail_paidEpisodeNotUnlocked_hidesAudioUrl(self):
        response = self.client.get(f"/api/v1/shows/{self.show.slug}")

        free, paid = response.data["episodes"]
        self.assertFalse(free["locked"])
        self.assertIsNotNone(free["audio_url"])
        self.assertTrue(paid["locked"])
        self.assertIsNone(paid["audio_url"])

    def test_episodeDetail_afterUnlock_returnsAudioUrl(self):
        paid = self.show.episodes.get(number=2)
        wallet_services.top_up(self.user, 100, "pay-1")
        wallet_services.unlock_episode(self.user, paid.id)

        response = self.client.get(f"/api/v1/episodes/{paid.id}")

        self.assertFalse(response.data["locked"])
        self.assertEqual(response.data["audio_url"], paid.audio_url)

    def test_listShows_unauthenticated_returns401(self):
        response = APIClient().get("/api/v1/shows")
        self.assertEqual(response.status_code, 401)


@override_settings(INTERNAL_API_TOKEN="test-token")
class InternalAccessTests(TestCase):
    def setUp(self):
        self.user = User.objects.create_user("listener", password="secret-pass-1")
        self.show = make_show()
        self.paid = self.show.episodes.get(number=2)

    def get_access(self, token="test-token", **params):
        return self.client.get("/internal/v1/access", params, HTTP_X_INTERNAL_TOKEN=token)

    def test_access_wrongToken_returns401(self):
        response = self.get_access(token="nope", user_id=self.user.id, episode_id=self.paid.id)
        self.assertEqual(response.status_code, 401)

    def test_access_paidEpisodeLocked_returnsNotAllowed(self):
        response = self.get_access(user_id=self.user.id, episode_id=self.paid.id)

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json(), {"allowed": False, "show_id": self.show.id, "duration_seconds": 600})

    def test_access_unknownEpisode_returns404(self):
        response = self.get_access(user_id=self.user.id, episode_id=9999)
        self.assertEqual(response.status_code, 404)


class EpisodeCompletedConsumerTests(TestCase):
    def test_applyEpisodeCompleted_sameEventTwice_countsOnce(self):
        episode = make_show().episodes.first()
        event = {"event_id": "evt-1", "user_id": 1, "episode_id": episode.id, "show_id": episode.show_id}

        self.assertTrue(apply_episode_completed(event))
        self.assertFalse(apply_episode_completed(event))

        episode.refresh_from_db()
        self.assertEqual(episode.completions, 1)
