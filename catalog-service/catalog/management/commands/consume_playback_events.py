import json
import logging

from django.conf import settings
from django.core.management.base import BaseCommand
from kafka import KafkaConsumer

from catalog.events import apply_episode_completed

logger = logging.getLogger(__name__)


class Command(BaseCommand):
    help = "Consume episode_completed events from playback-service and update episode completion counts."

    def handle(self, *args, **options):
        consumer = KafkaConsumer(
            settings.KAFKA_EPISODE_COMPLETED_TOPIC,
            bootstrap_servers=settings.KAFKA_BOOTSTRAP_SERVERS,
            group_id="catalog-service",
            enable_auto_commit=False,
            auto_offset_reset="earliest",
            value_deserializer=lambda v: json.loads(v.decode("utf-8")),
        )
        logger.info("consuming %s", settings.KAFKA_EPISODE_COMPLETED_TOPIC)

        for message in consumer:
            try:
                applied = apply_episode_completed(message.value)
                logger.info("event %s applied=%s", message.value.get("event_id"), applied)
            except (KeyError, TypeError):
                # A malformed message would otherwise block the partition forever; skip it.
                logger.exception("skipping malformed event at offset %s", message.offset)
            # Commit only after the DB transaction, so a crash re-delivers instead of losing the event.
            consumer.commit()
