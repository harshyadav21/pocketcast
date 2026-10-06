from django.contrib import admin
from django.urls import include, path

from catalog.views import InternalEpisodeAccessView

urlpatterns = [
    path("admin/", admin.site.urls),
    path("api-auth/", include("rest_framework.urls")),
    path("api/v1/", include("catalog.urls")),
    path("api/v1/", include("wallet.urls")),
    path("internal/v1/access", InternalEpisodeAccessView.as_view(), name="internal-access"),
]
