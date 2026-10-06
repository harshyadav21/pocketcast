from django.urls import path
from rest_framework.authtoken.views import obtain_auth_token

from .views import EpisodeDetailView, ShowDetailView, ShowListView, UnlockEpisodeView
from pocketcast.auth_views import RegisterView

urlpatterns = [
    path("auth/register", RegisterView.as_view(), name="register"),
    path("auth/token", obtain_auth_token, name="token"),
    path("shows", ShowListView.as_view(), name="show-list"),
    path("shows/<slug:slug>", ShowDetailView.as_view(), name="show-detail"),
    path("episodes/<int:pk>", EpisodeDetailView.as_view(), name="episode-detail"),
    path("episodes/<int:pk>/unlock", UnlockEpisodeView.as_view(), name="episode-unlock"),
]
