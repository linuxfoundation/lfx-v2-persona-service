// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package design

import (
	"goa.design/goa/v3/dsl"
)

var _ = dsl.API("persona", func() {
	dsl.Title("LFX v2 Persona Service")
	dsl.Description("Persona service providing NATS-based user persona aggregation with health endpoints")
	dsl.Version("1.0")
})

// Service describes the health check service
var _ = dsl.Service("persona-service", func() {
	dsl.Description("Persona service health endpoints")

	// Liveness probe endpoint
	dsl.Method("livez", func() {
		dsl.Description("Check if the service is alive.")
		dsl.Meta("swagger:generate", "false")
		dsl.Result(dsl.Bytes, func() {
			dsl.Example("OK")
		})
		dsl.HTTP(func() {
			dsl.GET("/livez")
			dsl.Response(dsl.StatusOK, func() {
				dsl.ContentType("text/plain")
			})
		})
	})

	// Readiness probe endpoint
	dsl.Method("readyz", func() {
		dsl.Description("Check if the service is ready to accept requests.")
		dsl.Meta("swagger:generate", "false")
		dsl.Result(dsl.Bytes, func() {
			dsl.Example("OK")
		})

		dsl.Error("ServiceUnavailable", dsl.String, "Service unavailable")

		dsl.HTTP(func() {
			dsl.GET("/readyz")
			dsl.Response(dsl.StatusOK, func() {
				dsl.ContentType("text/plain")
			})
			dsl.Response("ServiceUnavailable", dsl.StatusServiceUnavailable, func() {
				dsl.ContentType("text/plain")
			})
		})
	})
})
