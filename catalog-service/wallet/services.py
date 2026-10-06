from django.db import transaction

from catalog.models import Episode

from .exceptions import DuplicatePaymentReference, EpisodeIsFree, InsufficientCoins
from .models import CoinTransaction, EpisodeUnlock, Wallet


def get_wallet(user):
    wallet, _ = Wallet.objects.get_or_create(user=user)
    return wallet


def _lock_wallet(user):
    # Make sure the row exists, then take a row lock (SELECT ... FOR UPDATE) for the rest
    # of the transaction. Every balance change for this user queues behind this lock.
    get_wallet(user)
    return Wallet.objects.select_for_update().get(user=user)


def has_access(user_id, episode):
    if episode.is_free:
        return True
    return EpisodeUnlock.objects.filter(user_id=user_id, episode=episode).exists()


@transaction.atomic
def top_up(user, amount, payment_ref):
    """Credit coins after a successful payment. Retrying with the same payment_ref is a no-op."""
    wallet = _lock_wallet(user)

    existing = CoinTransaction.objects.filter(reference=payment_ref).first()
    if existing:
        if existing.wallet_id != wallet.id or existing.amount != amount:
            raise DuplicatePaymentReference()
        return existing, False

    wallet.balance += amount
    wallet.save(update_fields=["balance", "updated_at"])
    txn = CoinTransaction.objects.create(
        wallet=wallet,
        type=CoinTransaction.Type.CREDIT,
        amount=amount,
        balance_after=wallet.balance,
        reference=payment_ref,
    )
    return txn, True


@transaction.atomic
def unlock_episode(user, episode_id):
    """Spend coins to unlock a paid episode. Unlocking an already-unlocked episode never charges again."""
    episode = Episode.objects.select_related("show").get(pk=episode_id, show__is_published=True)
    if episode.is_free:
        raise EpisodeIsFree()

    wallet = _lock_wallet(user)

    # Checked after taking the lock, so two concurrent unlocks of the same episode
    # can't both get past this point.
    existing = EpisodeUnlock.objects.filter(user=user, episode=episode).first()
    if existing:
        return existing, False

    if wallet.balance < episode.coin_price:
        raise InsufficientCoins()

    wallet.balance -= episode.coin_price
    wallet.save(update_fields=["balance", "updated_at"])
    txn = CoinTransaction.objects.create(
        wallet=wallet,
        type=CoinTransaction.Type.DEBIT,
        amount=episode.coin_price,
        balance_after=wallet.balance,
        reference=f"unlock:{user.id}:{episode.id}",
    )
    unlock = EpisodeUnlock.objects.create(user=user, episode=episode, transaction=txn)
    return unlock, True
