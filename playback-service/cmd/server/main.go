package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/harshyadav21/pocketcast/playback-service/internal/api"
	"github.com/harshyadav21/pocketcast/playback-service/internal/catalog"
	"github.com/harshyadav21/pocketcast/playback-service/internal/config"
	"github.com/harshyadav21/pocketcast/playback-service/internal/events"
	"github.com/harshyadav21/pocketcast/playback-service/internal/progress"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := config.Load()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("redis unreachable", "addr", cfg.RedisAddr, "err", err)
		os.Exit(1)
	}
	cancel()

	publisher := events.NewKafkaPublisher(cfg.KafkaBrokers, cfg.KafkaTopic)
	catalogClient := catalog.NewClient(cfg.CatalogURL, cfg.InternalToken, cfg.AccessCacheTTL)
	svc := progress.NewService(progress.NewStore(rdb), catalogClient, publisher)

	gin.SetMode(gin.ReleaseMode)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(svc),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("playback-service listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	// Stop accepting requests, let in-flight ones finish, then flush Kafka and close Redis.
	slog.Info("shutting down")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	if err := publisher.Close(); err != nil {
		slog.Error("kafka close", "err", err)
	}
	if err := rdb.Close(); err != nil {
		slog.Error("redis close", "err", err)
	}
}
