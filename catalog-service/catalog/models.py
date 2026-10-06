from django.db import models


class Show(models.Model):
    class Genre(models.TextChoices):
        ROMANCE = "ROMANCE"
        THRILLER = "THRILLER"
        HORROR = "HORROR"
        FANTASY = "FANTASY"
        DRAMA = "DRAMA"

    title = models.CharField(max_length=200)
    slug = models.SlugField(max_length=200, unique=True)
    description = models.TextField(blank=True)
    genre = models.CharField(max_length=20, choices=Genre.choices)
    language = models.CharField(max_length=20, default="hindi")
    is_published = models.BooleanField(default=False)
    created_at = models.DateTimeField(auto_now_add=True)

    class Meta:
        indexes = [models.Index(fields=["genre", "language", "is_published"])]
        ordering = ["-created_at", "-id"]

    def __str__(self):
        return self.title


class Episode(models.Model):
    show = models.ForeignKey(Show, on_delete=models.CASCADE, related_name="episodes")
    number = models.PositiveIntegerField()
    title = models.CharField(max_length=200)
    duration_seconds = models.PositiveIntegerField()
    audio_url = models.URLField()
    is_free = models.BooleanField(default=False)
    coin_price = models.PositiveIntegerField(default=0)
    # Updated by the Kafka consumer from playback-service completion events.
    completions = models.PositiveBigIntegerField(default=0)

    class Meta:
        constraints = [
            models.UniqueConstraint(fields=["show", "number"], name="uniq_episode_number_per_show"),
        ]
        ordering = ["show_id", "number"]

    def __str__(self):
        return f"{self.show.title} - Ep {self.number}"


class ProcessedEvent(models.Model):
    """Event ids already applied by the consumer, so a redelivered Kafka message is a no-op."""

    event_id = models.CharField(max_length=64, unique=True)
    processed_at = models.DateTimeField(auto_now_add=True)
