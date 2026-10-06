package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/harshyadav21/pocketcast/playback-service/internal/catalog"
	"github.com/harshyadav21/pocketcast/playback-service/internal/events"
	"github.com/harshyadav21/pocketcast/playback-service/internal/progress"
)

type stubAccess struct {
	access catalog.Access
	err    error
}

func (s stubAccess) CheckAccess(context.Context, int64, int64) (catalog.Access, error) {
	return s.access, s.err
}

type noopPublisher struct{}

func (noopPublisher) PublishEpisodeCompleted(context.Context, events.EpisodeCompleted) error {
	return nil
}

func newTestRouter(t *testing.T, access stubAccess) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewRouter(progress.NewService(progress.NewStore(rdb), access, noopPublisher{}))
}

func do(r *gin.Engine, method, path, userID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSaveProgress_statusCodes(t *testing.T) {
	open := stubAccess{access: catalog.Access{Allowed: true, ShowID: 1, DurationSeconds: 600}}
	locked := stubAccess{access: catalog.Access{Allowed: false, DurationSeconds: 600}}
	missing := stubAccess{err: catalog.ErrEpisodeNotFound}

	tests := []struct {
		name   string
		access stubAccess
		userID string
		body   string
		want   int
	}{
		{"ok", open, "1", `{"episode_id": 5, "position_seconds": 30}`, http.StatusOK},
		{"position zero is valid", open, "1", `{"episode_id": 5, "position_seconds": 0}`, http.StatusOK},
		{"missing user header", open, "", `{"episode_id": 5, "position_seconds": 30}`, http.StatusUnauthorized},
		{"missing position", open, "1", `{"episode_id": 5}`, http.StatusBadRequest},
		{"locked episode", locked, "1", `{"episode_id": 5, "position_seconds": 30}`, http.StatusForbidden},
		{"unknown episode", missing, "1", `{"episode_id": 5, "position_seconds": 30}`, http.StatusNotFound},
		{"past duration", open, "1", `{"episode_id": 5, "position_seconds": 700}`, http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := do(newTestRouter(t, tc.access), http.MethodPost, "/v1/progress", tc.userID, tc.body)
			if w.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestGetProgress_afterSave_returnsPosition(t *testing.T) {
	r := newTestRouter(t, stubAccess{access: catalog.Access{Allowed: true, ShowID: 1, DurationSeconds: 600}})
	do(r, http.MethodPost, "/v1/progress", "1", `{"episode_id": 5, "position_seconds": 42}`)

	w := do(r, http.MethodGet, "/v1/progress/5", "1", "")

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"position_seconds":42`) {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if other := do(r, http.MethodGet, "/v1/progress/5", "2", ""); other.Code != http.StatusNotFound {
		t.Fatalf("another user's progress must not leak, got %d", other.Code)
	}
}

func TestTrending_noData_returnsEmptyList(t *testing.T) {
	w := do(newTestRouter(t, stubAccess{}), http.MethodGet, "/v1/trending", "", "")

	if w.Code != http.StatusOK || w.Body.String() != `{"items":[]}` {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}
