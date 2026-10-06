// Package events publishes playback events to Kafka.
package events

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"
)

type EpisodeCompleted struct {
	EventID     string    `json:"event_id"`
	UserID      int64     `json:"user_id"`
	EpisodeID   int64     `json:"episode_id"`
	ShowID      int64     `json:"show_id"`
	CompletedAt time.Time `json:"completed_at"`
}

type KafkaPublisher struct {
	writer *kafka.Writer
}

func NewKafkaPublisher(brokers []string, topic string) *KafkaPublisher {
	return &KafkaPublisher{
		writer: &kafka.Writer{
			Addr:  kafka.TCP(brokers...),
			Topic: topic,
			// Keyed by user so one user's events stay in order on a single partition.
			Balancer:               &kafka.Hash{},
			RequiredAcks:           kafka.RequireAll,
			AllowAutoTopicCreation: true,
			BatchTimeout:           10 * time.Millisecond,
		},
	}
}

func (p *KafkaPublisher) PublishEpisodeCompleted(ctx context.Context, e EpisodeCompleted) error {
	value, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(strconv.FormatInt(e.UserID, 10)),
		Value: value,
	})
}

func (p *KafkaPublisher) Close() error {
	return p.writer.Close()
}
