from django.conf import settings
from django.db import models

from catalog.models import Episode


class Wallet(models.Model):
    user = models.OneToOneField(settings.AUTH_USER_MODEL, on_delete=models.CASCADE, related_name="wallet")
    balance = models.PositiveIntegerField(default=0)
    updated_at = models.DateTimeField(auto_now=True)

    def __str__(self):
        return f"{self.user} ({self.balance} coins)"


class CoinTransaction(models.Model):
    """Append-only coin ledger. The balance on Wallet must always equal the last balance_after."""

    class Type(models.TextChoices):
        CREDIT = "CREDIT"
        DEBIT = "DEBIT"

    wallet = models.ForeignKey(Wallet, on_delete=models.PROTECT, related_name="transactions")
    type = models.CharField(max_length=10, choices=Type.choices)
    amount = models.PositiveIntegerField()
    balance_after = models.PositiveIntegerField()
    # Unique business reference (payment id for top-ups, "unlock:<user>:<episode>" for unlocks).
    # The unique index is what makes retries safe: a second insert with the same reference fails.
    reference = models.CharField(max_length=100, unique=True)
    created_at = models.DateTimeField(auto_now_add=True)

    class Meta:
        indexes = [models.Index(fields=["wallet", "-created_at"])]
        ordering = ["-created_at", "-id"]


class EpisodeUnlock(models.Model):
    user = models.ForeignKey(settings.AUTH_USER_MODEL, on_delete=models.CASCADE, related_name="unlocks")
    episode = models.ForeignKey(Episode, on_delete=models.CASCADE, related_name="unlocks")
    transaction = models.OneToOneField(CoinTransaction, on_delete=models.PROTECT)
    created_at = models.DateTimeField(auto_now_add=True)

    class Meta:
        constraints = [
            models.UniqueConstraint(fields=["user", "episode"], name="uniq_unlock_per_user_episode"),
        ]
