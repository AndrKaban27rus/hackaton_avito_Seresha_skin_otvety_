package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type Publisher interface {
	Publish(context.Context, string, any) error
}
type Kafka struct {
	writer *kafka.Writer
	logger *slog.Logger
}

func NewKafka(brokers, topic string, logger *slog.Logger) *Kafka {
	list := strings.Split(brokers, ",")
	return &Kafka{writer: &kafka.Writer{Addr: kafka.TCP(list...), Topic: topic, Balancer: &kafka.LeastBytes{}, Async: true}, logger: logger}
}
func (k *Kafka) Publish(ctx context.Context, key string, event any) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err = k.writer.WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: b, Time: time.Now()})
	if err != nil {
		k.logger.Warn("event publish failed", "error", err)
	}
	return err
}
func (k *Kafka) Close() error { return k.writer.Close() }
