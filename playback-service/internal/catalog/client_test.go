package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckAccess_allowed_isCachedAndSendsToken(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Internal-Token") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"allowed": true, "show_id": 3, "duration_seconds": 600}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret", time.Minute)
	for i := 0; i < 3; i++ {
		access, err := c.CheckAccess(context.Background(), 1, 2)
		if err != nil || !access.Allowed || access.ShowID != 3 {
			t.Fatalf("access=%+v err=%v", access, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("want 1 upstream call, got %d", calls.Load())
	}
}

func TestCheckAccess_denied_isNotCached(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"allowed": false, "show_id": 3, "duration_seconds": 600}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret", time.Minute)
	c.CheckAccess(context.Background(), 1, 2)
	c.CheckAccess(context.Background(), 1, 2)

	if calls.Load() != 2 {
		t.Fatalf("denied answers must not be cached; got %d upstream calls", calls.Load())
	}
}

func TestCheckAccess_unknownEpisode_returnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "secret", time.Minute).CheckAccess(context.Background(), 1, 99)
	if !errors.Is(err, ErrEpisodeNotFound) {
		t.Fatalf("want ErrEpisodeNotFound, got %v", err)
	}
}
