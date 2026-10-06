package progress

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/harshyadav21/pocketcast/playback-service/internal/catalog"
	"github.com/harshyadav21/pocketcast/playback-service/internal/events"
)

type fakeAccess struct {
	access catalog.Access
	err    error
}

func (f fakeAccess) CheckAccess(context.Context, int64, int64) (catalog.Access, error) {
	return f.access, f.err
}

type fakePublisher struct {
	mu     sync.Mutex
	events []events.EpisodeCompleted
	err    error
}

func (f *fakePublisher) PublishEpisodeCompleted(_ context.Context, e events.EpisodeCompleted) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

var allowed = catalog.Access{Allowed: true, ShowID: 7, DurationSeconds: 600}

func newService(t *testing.T, access AccessChecker, pub Publisher) *Service {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewService(NewStore(rdb), access, pub)
}

func TestSaveProgress_midEpisode_storesPositionWithoutCompleting(t *testing.T) {
	pub := &fakePublisher{}
	svc := newService(t, fakeAccess{access: allowed}, pub)
	ctx := context.Background()

	result, err := svc.SaveProgress(ctx, 1, 42, 120)
	if err != nil {
		t.Fatal(err)
	}

	pos, found, _ := svc.Position(ctx, 1, 42)
	if result.Completed || !found || pos != 120 || pub.count() != 0 {
		t.Fatalf("got completed=%v found=%v pos=%d events=%d", result.Completed, found, pos, pub.count())
	}
}

func TestSaveProgress_lockedEpisode_returnsForbidden(t *testing.T) {
	svc := newService(t, fakeAccess{access: catalog.Access{Allowed: false, DurationSeconds: 600}}, &fakePublisher{})

	_, err := svc.SaveProgress(context.Background(), 1, 42, 10)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestSaveProgress_positionPastDuration_returnsInvalidPosition(t *testing.T) {
	svc := newService(t, fakeAccess{access: allowed}, &fakePublisher{})

	_, err := svc.SaveProgress(context.Background(), 1, 42, 601)
	if !errors.Is(err, ErrInvalidPosition) {
		t.Fatalf("want ErrInvalidPosition, got %v", err)
	}
}

func TestSaveProgress_repeatedHeartbeatsAfterEnd_publishesOnce(t *testing.T) {
	pub := &fakePublisher{}
	svc := newService(t, fakeAccess{access: allowed}, pub)
	ctx := context.Background()

	for _, pos := range []int{570, 580, 600} { // 570 is exactly 95% of 600
		result, err := svc.SaveProgress(ctx, 1, 42, pos)
		if err != nil || !result.Completed {
			t.Fatalf("pos %d: completed=%v err=%v", pos, result.Completed, err)
		}
	}

	if pub.count() != 1 {
		t.Fatalf("want 1 event, got %d", pub.count())
	}
}

func TestSaveProgress_concurrentCompletionHeartbeats_publishesOnce(t *testing.T) {
	pub := &fakePublisher{}
	svc := newService(t, fakeAccess{access: allowed}, pub)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.SaveProgress(context.Background(), 1, 42, 600); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if pub.count() != 1 {
		t.Fatalf("want exactly 1 event from 20 concurrent heartbeats, got %d", pub.count())
	}
}

func TestSaveProgress_publishFails_retriesOnNextHeartbeat(t *testing.T) {
	pub := &fakePublisher{err: errors.New("kafka down")}
	svc := newService(t, fakeAccess{access: allowed}, pub)
	ctx := context.Background()

	result, err := svc.SaveProgress(ctx, 1, 42, 600)
	if err != nil || result.Completed {
		t.Fatalf("first heartbeat: completed=%v err=%v", result.Completed, err)
	}

	pub.err = nil
	result, _ = svc.SaveProgress(ctx, 1, 42, 600)
	if !result.Completed || pub.count() != 1 {
		t.Fatalf("retry: completed=%v events=%d", result.Completed, pub.count())
	}
}

func TestContinueListening_returnsMostRecentFirst(t *testing.T) {
	svc := newService(t, fakeAccess{access: allowed}, &fakePublisher{})
	ctx := context.Background()

	if _, err := svc.SaveProgress(ctx, 1, 10, 30); err != nil {
		t.Fatal(err)
	}
	// SavePosition scores by unix seconds; write the second one directly a second later.
	if err := svc.store.SavePosition(ctx, 1, 11, 45, timeNowPlus(1)); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.ContinueListening(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].EpisodeID != 11 || entries[0].PositionSeconds != 45 || entries[1].EpisodeID != 10 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestTrending_ranksShowsByCompletions(t *testing.T) {
	svc := newService(t, fakeAccess{}, &fakePublisher{})
	ctx := context.Background()
	now := timeNowPlus(0)

	for _, showID := range []int64{1, 2, 2, 3, 3, 3} {
		if err := svc.store.IncrTrending(ctx, showID, now); err != nil {
			t.Fatal(err)
		}
	}
	// Outside the 7-day window, so it must not count.
	if err := svc.store.IncrTrending(ctx, 1, now.AddDate(0, 0, -8)); err != nil {
		t.Fatal(err)
	}

	shows, err := svc.Trending(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(shows) != 2 || shows[0].ShowID != 3 || shows[0].Completions != 3 || shows[1].ShowID != 2 {
		t.Fatalf("unexpected trending: %+v", shows)
	}
}

func timeNowPlus(seconds int) time.Time {
	return time.Now().Add(time.Duration(seconds) * time.Second)
}
