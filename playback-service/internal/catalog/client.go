// Package catalog talks to catalog-service to check whether a user may play an episode.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var ErrEpisodeNotFound = errors.New("episode not found")

type Access struct {
	Allowed         bool  `json:"allowed"`
	ShowID          int64 `json:"show_id"`
	DurationSeconds int   `json:"duration_seconds"`
}

type cacheKey struct {
	userID, episodeID int64
}

type cacheEntry struct {
	access    Access
	expiresAt time.Time
}

// Client caches only "allowed" answers. Access never gets revoked in this system, so a cached
// yes stays correct; caching a no would keep blocking a user right after they unlock.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	ttl     time.Duration

	mu    sync.Mutex // guards cache; a plain map is enough because every access takes mu
	cache map[cacheKey]cacheEntry
}

const maxCacheEntries = 100_000

func NewClient(baseURL, token string, ttl time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 2 * time.Second},
		ttl:     ttl,
		cache:   make(map[cacheKey]cacheEntry),
	}
}

func (c *Client) CheckAccess(ctx context.Context, userID, episodeID int64) (Access, error) {
	key := cacheKey{userID, episodeID}
	if access, ok := c.cached(key); ok {
		return access, nil
	}

	access, err := c.fetch(ctx, userID, episodeID)
	if err != nil {
		return Access{}, err
	}
	if access.Allowed {
		c.store(key, access)
	}
	return access, nil
}

func (c *Client) fetch(ctx context.Context, userID, episodeID int64) (Access, error) {
	q := url.Values{}
	q.Set("user_id", strconv.FormatInt(userID, 10))
	q.Set("episode_id", strconv.FormatInt(episodeID, 10))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/access?"+q.Encode(), nil)
	if err != nil {
		return Access{}, err
	}
	req.Header.Set("X-Internal-Token", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return Access{}, fmt.Errorf("catalog request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Access{}, ErrEpisodeNotFound
	default:
		return Access{}, fmt.Errorf("catalog returned status %d", resp.StatusCode)
	}

	var access Access
	if err := json.NewDecoder(resp.Body).Decode(&access); err != nil {
		return Access{}, fmt.Errorf("decode catalog response: %w", err)
	}
	return access, nil
}

func (c *Client) cached(key cacheKey) (Access, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return Access{}, false
	}
	return entry.access, true
}

func (c *Client) store(key cacheKey, access Access) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= maxCacheEntries {
		c.evictExpiredLocked()
	}
	if len(c.cache) >= maxCacheEntries {
		return // still full of live entries; skip caching rather than grow without bound
	}
	c.cache[key] = cacheEntry{access: access, expiresAt: time.Now().Add(c.ttl)}
}

func (c *Client) evictExpiredLocked() {
	now := time.Now()
	for k, e := range c.cache {
		if now.After(e.expiresAt) {
			delete(c.cache, k)
		}
	}
}
