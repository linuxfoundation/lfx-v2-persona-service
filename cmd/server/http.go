// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	personaservice "github.com/linuxfoundation/lfx-v2-persona-service/gen/persona_service"
	personaserver "github.com/linuxfoundation/lfx-v2-persona-service/gen/http/persona_service/server"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"goa.design/clue/debug"
	goahttp "goa.design/goa/v3/http"
)

// handleHTTPServer starts the HTTP server for health check endpoints.
func handleHTTPServer(ctx context.Context, host string, personaEndpoints *personaservice.Endpoints, wg *sync.WaitGroup, errc chan<- error, dbg bool) {

	var (
		dec = goahttp.RequestDecoder
		enc = goahttp.ResponseEncoder
	)

	var mux goahttp.Muxer
	{
		mux = goahttp.NewMuxer()
		if dbg {
			debug.MountPprofHandlers(debug.Adapt(mux))
			debug.MountDebugLogEnabler(debug.Adapt(mux))
		}
	}

	var (
		personaServer *personaserver.Server
	)
	{
		eh := errorHandler(ctx)
		personaServer = personaserver.New(personaEndpoints, mux, dec, enc, eh, nil)
	}
	personaserver.Mount(mux, personaServer)

	var handler http.Handler = mux
	if dbg {
		handler = debug.HTTP()(handler)
	}
	handler = otelhttp.NewHandler(handler, "persona-service")

	srv := &http.Server{Addr: host, Handler: handler, ReadHeaderTimeout: time.Second * 60}
	for _, m := range personaServer.Mounts {
		slog.InfoContext(ctx, "HTTP endpoint mounted",
			"method", m.Method,
			"verb", m.Verb,
			"pattern", m.Pattern,
		)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		go func() {
			<-ctx.Done()
			slog.InfoContext(ctx, "shutting down HTTP server", "host", host)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := srv.Shutdown(ctx); err != nil {
				slog.ErrorContext(ctx, "HTTP server shutdown error", "error", err)
			}
		}()

		slog.InfoContext(ctx, "HTTP server listening", "host", host)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errc <- err
		}
	}()
}

// errorHandler returns a function that writes and logs the given error.
func errorHandler(logCtx context.Context) func(context.Context, http.ResponseWriter, error) {
	return func(ctx context.Context, w http.ResponseWriter, err error) {
		slog.ErrorContext(logCtx, "HTTP error occurred", "error", err)
	}
}
