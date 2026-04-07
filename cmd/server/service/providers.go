// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/config"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/cdp"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/nats"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/service"
	"github.com/linuxfoundation/lfx-v2-persona-service/pkg/constants"
)

var (
	natsClient *nats.NATSClient
	appConfig  config.Config
	natsDoOnce sync.Once
)

func natsInit(ctx context.Context) {

	natsDoOnce.Do(func() {
		appConfig = config.Load()

		natsTimeout := os.Getenv("NATS_TIMEOUT")
		if natsTimeout == "" {
			natsTimeout = "10s"
		}
		natsTimeoutDuration, err := time.ParseDuration(natsTimeout)
		if err != nil {
			log.Fatalf("invalid NATS timeout duration: %v", err)
		}

		natsMaxReconnect := os.Getenv("NATS_MAX_RECONNECT")
		if natsMaxReconnect == "" {
			natsMaxReconnect = "3"
		}
		natsMaxReconnectInt, err := strconv.Atoi(natsMaxReconnect)
		if err != nil {
			log.Fatalf("invalid NATS max reconnect value %s: %v", natsMaxReconnect, err)
		}

		natsReconnectWait := os.Getenv("NATS_RECONNECT_WAIT")
		if natsReconnectWait == "" {
			natsReconnectWait = "2s"
		}
		natsReconnectWaitDuration, err := time.ParseDuration(natsReconnectWait)
		if err != nil {
			log.Fatalf("invalid NATS reconnect wait duration %s : %v", natsReconnectWait, err)
		}

		natsConfig := nats.Config{
			URL:           appConfig.NATSURL,
			Timeout:       natsTimeoutDuration,
			MaxReconnect:  natsMaxReconnectInt,
			ReconnectWait: natsReconnectWaitDuration,
		}

		client, errNewClient := nats.NewClient(ctx, natsConfig)
		if errNewClient != nil {
			log.Fatalf("failed to create NATS client: %v", errNewClient)
		}
		natsClient = client

		// Attempt to initialize the persona-cache KV bucket.
		// This is non-fatal — the bucket may not exist yet in development.
		if err := natsClient.KeyValueStore(ctx, constants.KVBucketNamePersonaCache); err != nil {
			slog.WarnContext(ctx, "persona-cache KV bucket not available — caching will be disabled until bucket is created",
				"error", err,
				"bucket", constants.KVBucketNamePersonaCache,
			)
		} else {
			slog.InfoContext(ctx, "NATS KV store initialized",
				"bucket", constants.KVBucketNamePersonaCache,
			)
		}
	})
}

// QueueSubscriptions starts all NATS subscriptions.
func QueueSubscriptions(ctx context.Context) error {
	slog.DebugContext(ctx, "starting NATS subscriptions")

	natsInit(ctx)

	// Build handler options based on config.
	var handlerOpts []service.PersonaHandlerOption

	if appConfig.CDPEnabled {
		cdpClient, cdpCache, err := newCDPClient(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "failed to create CDP client — CDP sources will be disabled", "error", err)
		} else {
			handlerOpts = append(handlerOpts, service.WithCDP(cdpClient, cdpCache))
			slog.InfoContext(ctx, "CDP source enabled")
		}
	}

	messageHandlerService := &MessageHandlerService{
		messageHandler: service.NewPersonaHandler(handlerOpts...),
	}

	client := getNATSClient()
	if client == nil {
		return fmt.Errorf("NATS client not initialized")
	}

	subjects := map[string]func(context.Context, port.TransportMessenger){
		constants.PersonaGetSubject: messageHandlerService.HandleMessage,
	}

	for subject, handler := range subjects {
		slog.DebugContext(ctx, "subscribing to NATS subject", "subject", subject)
		if _, err := client.SubscribeWithTransportMessenger(ctx, subject, constants.PersonaServiceQueue, handler); err != nil {
			slog.ErrorContext(ctx, "failed to subscribe to NATS subject",
				"error", err,
				"subject", subject,
			)
			return fmt.Errorf("failed to subscribe to subject %s: %w", subject, err)
		}
	}

	slog.DebugContext(ctx, "NATS subscriptions started successfully")
	return nil
}

// newCDPClient creates a CDP API client and cache from the loaded config.
func newCDPClient(ctx context.Context) (*cdp.Client, *cdp.Cache, error) {
	tokenProv, err := cdp.NewTokenProvider(cdp.TokenProviderConfig{
		IssuerBaseURL:    appConfig.Auth0IssuerBaseURL,
		ClientID:         appConfig.Auth0ClientID,
		Audience:         appConfig.CDPAudience,
		PrivateKeyBase64: appConfig.Auth0M2MPrivateBase64Key,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create CDP token provider: %w", err)
	}

	client := cdp.NewClient(cdp.ClientConfig{
		BaseURL:   appConfig.CDPBaseURL,
		TokenProv: tokenProv,
	})

	// Use the persona-cache KV store if available.
	var cache *cdp.Cache
	kvStore, ok := natsClient.GetKVStore(constants.KVBucketNamePersonaCache)
	if ok {
		cache = cdp.NewCache(kvStore)
		slog.InfoContext(ctx, "CDP cache enabled via NATS KV")
	} else {
		cache = cdp.NewCache(nil)
		slog.WarnContext(ctx, "CDP cache disabled — persona-cache KV bucket not available")
	}

	return client, cache, nil
}

// getNATSClient returns the initialized NATS client.
func getNATSClient() *nats.NATSClient {
	return natsClient
}
