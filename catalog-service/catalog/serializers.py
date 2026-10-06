from rest_framework import serializers

from .models import Episode, Show


class EpisodeSerializer(serializers.ModelSerializer):
    locked = serializers.SerializerMethodField()
    audio_url = serializers.SerializerMethodField()

    class Meta:
        model = Episode
        fields = [
            "id", "number", "title", "duration_seconds", "is_free", "coin_price", "completions", "locked", "audio_url",
        ]

    def _is_unlocked(self, episode):
        return episode.is_free or episode.id in self.context["unlocked_ids"]

    def get_locked(self, episode):
        return not self._is_unlocked(episode)

    def get_audio_url(self, episode):
        return episode.audio_url if self._is_unlocked(episode) else None


class ShowListSerializer(serializers.ModelSerializer):
    class Meta:
        model = Show
        fields = ["id", "slug", "title", "genre", "language"]


class ShowDetailSerializer(serializers.ModelSerializer):
    episodes = EpisodeSerializer(many=True)

    class Meta:
        model = Show
        fields = ["id", "slug", "title", "description", "genre", "language", "episodes"]
