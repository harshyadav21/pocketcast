from django.core.management.base import BaseCommand

from catalog.models import Episode, Show

SHOWS = [
    ("Insta Millionaire", "DRAMA"),
    ("The Haunted Haveli", "HORROR"),
    ("Saving Nora", "ROMANCE"),
]


class Command(BaseCommand):
    help = "Create a few published shows with free and paid episodes for local testing."

    def handle(self, *args, **options):
        for title, genre in SHOWS:
            slug = title.lower().replace(" ", "-")
            show, created = Show.objects.get_or_create(
                slug=slug,
                defaults={"title": title, "genre": genre, "is_published": True},
            )
            if not created:
                continue
            for number in range(1, 11):
                Episode.objects.create(
                    show=show,
                    number=number,
                    title=f"{title} - Episode {number}",
                    duration_seconds=600,
                    audio_url=f"https://cdn.example.com/{slug}/{number}.mp3",
                    is_free=number <= 3,
                    coin_price=0 if number <= 3 else 25,
                )
            self.stdout.write(f"created {title}")
