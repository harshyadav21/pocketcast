// Package api exposes the playback HTTP API with Gin.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/harshyadav21/pocketcast/playback-service/internal/catalog"
	"github.com/harshyadav21/pocketcast/playback-service/internal/metrics"
	"github.com/harshyadav21/pocketcast/playback-service/internal/progress"
)

const (
	userIDKey    = "userID"
	defaultLimit = 10
	maxLimit     = 50
)

type Handler struct {
	progress *progress.Service
}

type saveProgressRequest struct {
	EpisodeID       int64 `json:"episode_id" binding:"required,gt=0"`
	PositionSeconds *int  `json:"position_seconds" binding:"required"`
}

func NewRouter(svc *progress.Service) *gin.Engine {
	h := &Handler{progress: svc}

	r := gin.New()
	r.Use(gin.Recovery(), observe())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := r.Group("/v1")
	v1.GET("/trending", h.trending)

	user := v1.Group("", requireUser())
	user.POST("/progress", h.saveProgress)
	user.GET("/progress/:episodeID", h.getProgress)
	user.GET("/continue-listening", h.continueListening)

	return r
}

func (h *Handler) saveProgress(c *gin.Context) {
	var req saveProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.progress.SaveProgress(c.Request.Context(), c.GetInt64(userIDKey), req.EpisodeID, *req.PositionSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) getProgress(c *gin.Context) {
	episodeID, err := strconv.ParseInt(c.Param("episodeID"), 10, 64)
	if err != nil || episodeID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "episodeID must be a positive integer"})
		return
	}

	position, found, err := h.progress.Position(c.Request.Context(), c.GetInt64(userIDKey), episodeID)
	if err != nil {
		writeError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no progress for this episode"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"episode_id": episodeID, "position_seconds": position})
}

func (h *Handler) continueListening(c *gin.Context) {
	entries, err := h.progress.ContinueListening(c.Request.Context(), c.GetInt64(userIDKey), parseLimit(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": nonNil(entries)})
}

func (h *Handler) trending(c *gin.Context) {
	shows, err := h.progress.Trending(c.Request.Context(), parseLimit(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": nonNil(shows)})
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, progress.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, progress.ErrInvalidPosition):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, catalog.ErrEpisodeNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		slog.Error("request failed", "path", c.FullPath(), "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// requireUser reads the user id set by the API gateway after it has validated the auth token.
func requireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := strconv.ParseInt(c.GetHeader("X-User-ID"), 10, 64)
		if err != nil || userID <= 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid X-User-ID"})
			return
		}
		c.Set(userIDKey, userID)
		c.Next()
	}
}

func observe() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched" // keeps label cardinality bounded for random 404 paths
		}
		metrics.HTTPRequests.WithLabelValues(route, c.Request.Method, strconv.Itoa(c.Writer.Status())).Inc()
		metrics.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}
}

func parseLimit(c *gin.Context) int {
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	return min(limit, maxLimit)
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
