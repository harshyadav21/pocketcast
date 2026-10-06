from django.db import transaction
from django.db.models import F

from .models import Episode, ProcessedEvent


@transaction.atomic
def apply_episode_completed(event):
    """Count one completion for the episode. Returns False if this event was already applied.

    Kafka gives at-least-once delivery, so the same event can arrive twice (consumer crash
    before the offset commit, rebalance). Recording the event id in the same DB transaction
    as the counter update makes the consumer idempotent.
    """
    _, created = ProcessedEvent.objects.get_or_create(event_id=event["event_id"])
    if not created:
        return False
    Episode.objects.filter(pk=event["episode_id"]).update(completions=F("completions") + 1)
    return True
