// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package nats

import (
	"context"
	"log/slog"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-persona-service/pkg/constants"
	"github.com/linuxfoundation/lfx-v2-persona-service/pkg/errors"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSClient wraps the NATS connection and provides messaging and KV operations.
type NATSClient struct {
	conn    *nats.Conn
	config  Config
	kvStore map[string]jetstream.KeyValue
}

// Close gracefully closes the NATS connection.
func (c *NATSClient) Close() error {
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

// IsReady checks if the NATS client is ready.
func (c *NATSClient) IsReady(ctx context.Context) error {
	if c.conn == nil {
		return errors.NewServiceUnavailable("NATS client is not initialized or not connected")
	}
	if !c.conn.IsConnected() || c.conn.IsDraining() {
		return errors.NewServiceUnavailable("NATS client is not ready, connection is not established or is draining")
	}
	return nil
}

// KeyValueStore creates a JetStream client and gets the key-value store for
// the given bucket. The bucket must already exist on the server.
func (c *NATSClient) KeyValueStore(ctx context.Context, bucketName string) error {
	js, err := jetstream.New(c.conn)
	if err != nil {
		slog.ErrorContext(ctx, "error creating NATS JetStream client",
			"error", err,
			"nats_url", c.conn.ConnectedUrl(),
		)
		return err
	}
	kvStore, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		slog.ErrorContext(ctx, "error getting NATS JetStream key-value store",
			"error", err,
			"nats_url", c.conn.ConnectedUrl(),
			"bucket", bucketName,
		)
		return err
	}

	if c.kvStore == nil {
		c.kvStore = make(map[string]jetstream.KeyValue)
	}
	c.kvStore[bucketName] = kvStore
	return nil
}

// GetKVStore returns the KV store for a given bucket name.
func (c *NATSClient) GetKVStore(bucketName string) (jetstream.KeyValue, bool) {
	if c.kvStore == nil {
		return nil, false
	}
	kvStore, exists := c.kvStore[bucketName]
	return kvStore, exists
}

// Request sends a NATS request and waits for a reply.
func (c *NATSClient) Request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	if err := c.IsReady(ctx); err != nil {
		return nil, err
	}

	msg, err := c.conn.RequestWithContext(ctx, subject, data)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

// SubscribeWithTransportMessenger subscribes to a subject with proper TransportMessenger handling.
func (c *NATSClient) SubscribeWithTransportMessenger(ctx context.Context, subject string, queueName string, handler func(context.Context, port.TransportMessenger)) (*nats.Subscription, error) {

	if err := c.IsReady(ctx); err != nil {
		return nil, err
	}

	return c.conn.QueueSubscribe(subject, queueName, func(msg *nats.Msg) {
		transportMsg := NewTransportMessenger(msg)

		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "panic in NATS handler",
					"subject", subject,
					"queue", queueName,
					"panic", r,
				)
			}
		}()

		handler(ctx, transportMsg)
	})
}

// NewClient creates a new NATS client with the given configuration.
func NewClient(ctx context.Context, config Config) (*NATSClient, error) {
	slog.InfoContext(ctx, "creating NATS client",
		"url", config.URL,
		"timeout", config.Timeout,
	)

	if config.URL == "" {
		return nil, errors.NewUnexpected("NATS URL is required")
	}

	opts := []nats.Option{
		nats.Name(constants.ServiceName),
		nats.Timeout(config.Timeout),
		nats.MaxReconnects(config.MaxReconnect),
		nats.ReconnectWait(config.ReconnectWait),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			slog.WarnContext(ctx, "NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			slog.InfoContext(ctx, "NATS reconnected", "url", nc.ConnectedUrl())
		}),
		nats.ErrorHandler(func(_ *nats.Conn, s *nats.Subscription, err error) {
			if s != nil {
				slog.With("error", err, "subject", s.Subject, "queue", s.Queue).Error("async NATS error")
			} else {
				slog.With("error", err).Error("async NATS error outside subscription")
			}
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			slog.InfoContext(ctx, "NATS connection closed")
		}),
	}

	conn, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, errors.NewServiceUnavailable("failed to connect to NATS", err)
	}

	client := &NATSClient{
		conn:   conn,
		config: config,
	}

	slog.InfoContext(ctx, "NATS client created successfully",
		"connected_url", conn.ConnectedUrl(),
		"status", conn.Status(),
	)

	return client, nil
}
