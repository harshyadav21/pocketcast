import hmac

from django.conf import settings
from django.shortcuts import get_object_or_404
from rest_framework import generics, status
from rest_framework.permissions import AllowAny
from rest_framework.response import Response
from rest_framework.views import APIView

from wallet import services as wallet_services
from wallet.models import EpisodeUnlock

from .models import Episode, Show
from .serializers import EpisodeSerializer, ShowDetailSerializer, ShowListSerializer


def unlocked_episode_ids(user):
    return set(EpisodeUnlock.objects.filter(user=user).values_list("episode_id", flat=True))


class ShowListView(generics.ListAPIView):
    serializer_class = ShowListSerializer

    def get_queryset(self):
        qs = Show.objects.filter(is_published=True)
        genre = self.request.query_params.get("genre")
        language = self.request.query_params.get("language")
        if genre:
            qs = qs.filter(genre=genre.upper())
        if language:
            qs = qs.filter(language=language.lower())
        return qs


class ShowDetailView(generics.RetrieveAPIView):
    serializer_class = ShowDetailSerializer
    lookup_field = "slug"
    queryset = Show.objects.filter(is_published=True).prefetch_related("episodes")

    def get_serializer_context(self):
        context = super().get_serializer_context()
        context["unlocked_ids"] = unlocked_episode_ids(self.request.user)
        return context


class EpisodeDetailView(generics.RetrieveAPIView):
    serializer_class = EpisodeSerializer
    queryset = Episode.objects.filter(show__is_published=True)

    def get_serializer_context(self):
        context = super().get_serializer_context()
        context["unlocked_ids"] = unlocked_episode_ids(self.request.user)
        return context


class UnlockEpisodeView(APIView):
    def post(self, request, pk):
        get_object_or_404(Episode, pk=pk, show__is_published=True)
        unlock, created = wallet_services.unlock_episode(request.user, pk)
        return Response(
            {
                "episode_id": unlock.episode_id,
                "coins_spent": unlock.transaction.amount if created else 0,
                "balance": unlock.transaction.wallet.balance,
                "already_unlocked": not created,
            },
            status=status.HTTP_201_CREATED if created else status.HTTP_200_OK,
        )


class InternalEpisodeAccessView(APIView):
    """Called by playback-service before it accepts progress for an episode."""

    authentication_classes = []
    permission_classes = [AllowAny]

    def get(self, request):
        token = request.headers.get("X-Internal-Token", "")
        if not hmac.compare_digest(token, settings.INTERNAL_API_TOKEN):
            return Response({"detail": "Invalid internal token."}, status=status.HTTP_401_UNAUTHORIZED)

        try:
            user_id = int(request.query_params["user_id"])
            episode_id = int(request.query_params["episode_id"])
        except (KeyError, ValueError):
            return Response({"detail": "user_id and episode_id are required integers."}, status=400)

        episode = get_object_or_404(Episode, pk=episode_id, show__is_published=True)
        return Response(
            {
                "allowed": wallet_services.has_access(user_id, episode),
                "show_id": episode.show_id,
                "duration_seconds": episode.duration_seconds,
            }
        )
