from django.contrib import admin

from .models import CoinTransaction, EpisodeUnlock, Wallet


@admin.register(Wallet)
class WalletAdmin(admin.ModelAdmin):
    list_display = ["user", "balance", "updated_at"]
    search_fields = ["user__username"]
    # Balance only changes through wallet.services so the ledger stays consistent.
    readonly_fields = ["balance"]


@admin.register(CoinTransaction)
class CoinTransactionAdmin(admin.ModelAdmin):
    list_display = ["wallet", "type", "amount", "balance_after", "reference", "created_at"]
    list_filter = ["type"]
    search_fields = ["reference"]


@admin.register(EpisodeUnlock)
class EpisodeUnlockAdmin(admin.ModelAdmin):
    list_display = ["user", "episode", "created_at"]
