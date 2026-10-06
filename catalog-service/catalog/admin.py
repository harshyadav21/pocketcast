from django.contrib import admin

from .models import Episode, Show


class EpisodeInline(admin.TabularInline):
    model = Episode
    extra = 0
    fields = ["number", "title", "duration_seconds", "audio_url", "is_free", "coin_price", "completions"]
    readonly_fields = ["completions"]


@admin.register(Show)
class ShowAdmin(admin.ModelAdmin):
    list_display = ["title", "genre", "language", "is_published", "created_at"]
    list_filter = ["genre", "language", "is_published"]
    search_fields = ["title"]
    prepopulated_fields = {"slug": ["title"]}
    inlines = [EpisodeInline]
