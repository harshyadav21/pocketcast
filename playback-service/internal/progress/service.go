// Package progress records listening progress, detects episode completion and serves trending shows.
package progress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/harshyadav21/pocketcast/playback-service/internal/catalog"
	"github.com/harshyadav21/pocketcast/playback-service/internal/events"
	"github.com/harshyadav21/pocketcast/playback-service/internal/metrics"
)

var (
	ErrForbidden       = errors.New("episode is locked for this user")
	ErrInvalidPosition = errors.New("position is outside the episode duration")
)

// An episode counts as completed once the listener reaches 95% of it,
// so skipping the end credits still counts.
const completionPercent = 95

type AccessChecker interface {
	CheckAccess(ctx context.Context, userID, episodeID int64) (catalog.Access, error)
}

type Publisher interface {
	PublishEpisodeCompleted(ctx context.Context, e events.EpisodeCompleted) error
}

type Entry struct {
	EpisodeID       int64     `json:"episode_id"`
	PositionSeconds int       `json:"position_seconds"`
	LastPlayedAt    time.Time `json:"last_played_at"`
}

type ShowScore struct {
	ShowID      int64 `json:"show_id"`
	Completions int64 `json:"completions"`
}

type SaveResult struct {
	Completed bool `json:"completed"`
}

type Service struct {
	store     *Store
	access    AccessChecker
	publisher Publisher
}

func NewService(store *Store, access AccessChecker, publisher Publisher) *Service {
	return &Service{store: store, access: access, publisher: publisher}
}

// SaveProgress is called by the app every few seconds while an episode plays (a heartbeat).
func (s *Service) SaveProgress(ctx context.Context, userID, episodeID int64, position int) (SaveResult, error) {
	access, err := s.access.CheckAccess(ctx, userID, episodeID)
	if err != nil {
		return SaveResult{}, err
	}
	if !access.Allowed {
		return SaveResult{}, ErrForbidden
	}
	if position < 0 || position > access.DurationSeconds {
		return SaveResult{}, ErrInvalidPosition
	}

	now := time.Now()
	if err := s.store.SavePosition(ctx, userID, episodeID, position, now); err != nil {
		return SaveResult{}, fmt.Errorf("save position: %w", err)
	}

	if position*100 < access.DurationSeconds*completionPercent {
		return SaveResult{}, nil
	}
	return s.markCompleted(ctx, userID, episodeID, access.ShowID, now)
}

func (s *Service) markCompleted(ctx context.Context, userID, episodeID, showID int64, now time.Time) (SaveResult, error) {
	first, err := s.store.MarkCompleted(ctx, userID, episodeID)
	if err != nil {
		return SaveResult{}, fmt.Errorf("mark completed: %w", err)
	}
	if !first {
		return SaveResult{Completed: true}, nil
	}

	event := events.EpisodeCompleted{
		EventID:     uuid.NewString(),
		UserID:      userID,
		EpisodeID:   episodeID,
		ShowID:      showID,
		CompletedAt: now.UTC(),
	}
	if err := s.publisher.PublishEpisodeCompleted(ctx, event); err != nil {
		// The position is already saved, so the request still succeeds. Clearing the marker
		// lets the next heartbeat try the publish again instead of losing the event.
		metrics.PublishFailures.Inc()
		slog.Error("publish episode_completed failed", "user", userID, "episode", episodeID, "err", err)
		if err := s.store.UnmarkCompleted(ctx, userID, episodeID); err != nil {
			slog.Error("unmark completed failed", "user", userID, "episode", episodeID, "err", err)
		}
		return SaveResult{}, nil
	}

	metrics.EpisodesCompleted.Inc()
	if err := s.store.IncrTrending(ctx, showID, now); err != nil {
		slog.Error("increment trending failed", "show", showID, "err", err)
	}
	return SaveResult{Completed: true}, nil
}

func (s *Service) Position(ctx context.Context, userID, episodeID int64) (int, bool, error) {
	return s.store.Position(ctx, userID, episodeID)
}

func (s *Service) ContinueListening(ctx context.Context, userID int64, limit int) ([]Entry, error) {
	return s.store.Recent(ctx, userID, limit)
}

func (s *Service) Trending(ctx context.Context, limit int) ([]ShowScore, error) {
	return s.store.Trending(ctx, time.Now(), limit)
}

func sortByScoreDesc(scores []ShowScore) {
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Completions != scores[j].Completions {
			return scores[i].Completions > scores[j].Completions
		}
		return scores[i].ShowID < scores[j].ShowID
	})
}
