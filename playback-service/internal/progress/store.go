package progress

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	recentLimit  = 50
	progressTTL  = 90 * 24 * time.Hour
	completedTTL = 365 * 24 * time.Hour
	trendingTTL  = 8 * 24 * time.Hour
	trendingDays = 7
	dayKeyLayout = "2006-01-02"
)

// Redis layout:
//
//	progress:{user}            HASH   episodeID -> position seconds
//	recent:{user}              ZSET   episodeID scored by last-played unix time (capped at 50)
//	completed:{user}:{episode} STRING set once with SETNX, the "first completion" marker
//	trending:{yyyy-mm-dd}      ZSET   showID scored by completions that day
type Store struct {
	rdb *redis.Client
}

func NewStore(rdb *redis.Client) *Store {
	return &Store{rdb: rdb}
}

func progressKey(userID int64) string { return fmt.Sprintf("progress:%d", userID) }
func recentKey(userID int64) string   { return fmt.Sprintf("recent:%d", userID) }
func completedKey(userID, episodeID int64) string {
	return fmt.Sprintf("completed:%d:%d", userID, episodeID)
}
func trendingKey(day time.Time) string { return "trending:" + day.UTC().Format(dayKeyLayout) }

func (s *Store) SavePosition(ctx context.Context, userID, episodeID int64, position int, at time.Time) error {
	ep := strconv.FormatInt(episodeID, 10)
	// MULTI/EXEC so the position and the "recently played" entry are written together.
	_, err := s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, progressKey(userID), ep, position)
		p.Expire(ctx, progressKey(userID), progressTTL)
		p.ZAdd(ctx, recentKey(userID), redis.Z{Score: float64(at.Unix()), Member: ep})
		p.ZRemRangeByRank(ctx, recentKey(userID), 0, -(recentLimit + 1))
		p.Expire(ctx, recentKey(userID), progressTTL)
		return nil
	})
	return err
}

func (s *Store) Position(ctx context.Context, userID, episodeID int64) (int, bool, error) {
	v, err := s.rdb.HGet(ctx, progressKey(userID), strconv.FormatInt(episodeID, 10)).Int()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}

func (s *Store) Recent(ctx context.Context, userID int64, limit int) ([]Entry, error) {
	members, err := s.rdb.ZRevRangeWithScores(ctx, recentKey(userID), 0, int64(limit-1)).Result()
	if err != nil || len(members) == 0 {
		return nil, err
	}

	fields := make([]string, len(members))
	for i, m := range members {
		fields[i] = m.Member.(string)
	}
	positions, err := s.rdb.HMGet(ctx, progressKey(userID), fields...).Result()
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(members))
	for i, m := range members {
		episodeID, _ := strconv.ParseInt(fields[i], 10, 64)
		position := 0
		if s, ok := positions[i].(string); ok {
			position, _ = strconv.Atoi(s)
		}
		entries = append(entries, Entry{
			EpisodeID:       episodeID,
			PositionSeconds: position,
			LastPlayedAt:    time.Unix(int64(m.Score), 0).UTC(),
		})
	}
	return entries, nil
}

// MarkCompleted returns true only for the first caller, even with concurrent heartbeats,
// because SETNX is atomic in Redis.
func (s *Store) MarkCompleted(ctx context.Context, userID, episodeID int64) (bool, error) {
	return s.rdb.SetNX(ctx, completedKey(userID, episodeID), 1, completedTTL).Result()
}

func (s *Store) UnmarkCompleted(ctx context.Context, userID, episodeID int64) error {
	return s.rdb.Del(ctx, completedKey(userID, episodeID)).Err()
}

func (s *Store) IncrTrending(ctx context.Context, showID int64, at time.Time) error {
	key := trendingKey(at)
	_, err := s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZIncrBy(ctx, key, 1, strconv.FormatInt(showID, 10))
		p.Expire(ctx, key, trendingTTL)
		return nil
	})
	return err
}

// Trending sums the last 7 daily sorted sets, so old completions fall out of the window.
func (s *Store) Trending(ctx context.Context, now time.Time, limit int) ([]ShowScore, error) {
	keys := make([]string, trendingDays)
	for i := range keys {
		keys[i] = trendingKey(now.AddDate(0, 0, -i))
	}
	members, err := s.rdb.ZUnionWithScores(ctx, redis.ZStore{Keys: keys}).Result()
	if err != nil {
		return nil, err
	}

	scores := make([]ShowScore, 0, len(members))
	for _, m := range members {
		showID, _ := strconv.ParseInt(m.Member.(string), 10, 64)
		scores = append(scores, ShowScore{ShowID: showID, Completions: int64(m.Score)})
	}
	sortByScoreDesc(scores)
	if len(scores) > limit {
		scores = scores[:limit]
	}
	return scores, nil
}
