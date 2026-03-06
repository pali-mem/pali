# Go Client SDK (`pkg/client`)

This document describes the Go client in `pkg/client` for calling Pali API endpoints.

## File Layout

- `pkg/client/client.go`: client construction and options
- `pkg/client/types.go`: request/response models
- `pkg/client/transport.go`: shared HTTP + JSON transport helpers
- `pkg/client/errors.go`: API error type and parsing
- `pkg/client/health.go`: health endpoint methods
- `pkg/client/tenant.go`: tenant endpoint methods
- `pkg/client/memory.go`: memory endpoint methods

This split is idiomatic Go: multiple files in one package, organized by concern.

## Quick Start

```go
package main

import (
	"context"
	"log"

	"github.com/vein05/pali/pkg/client"
)

func main() {
	c, err := client.NewClient("http://127.0.0.1:8080")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	_, err = c.CreateTenant(ctx, client.CreateTenantRequest{
		ID:   "tenant_1",
		Name: "Tenant One",
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

## Auth

When server auth is enabled, set a bearer token:

```go
c.SetBearerToken("<jwt>")
```

You can also set it during construction:

```go
c, err := client.NewClient(
	"http://127.0.0.1:8080",
	client.WithBearerToken("<jwt>"),
)
```

## Error Handling

Non-2xx responses return `*client.APIError`:

```go
import (
	"errors"
	"log"

	"github.com/vein05/pali/pkg/client"
)

if err != nil {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		log.Printf("status=%d message=%s", apiErr.StatusCode, apiErr.Message)
	}
}
```

The message is parsed from server JSON errors (`{"error":"..."}`) when available.

## GoDoc

`pkg/client` includes package-level and exported-symbol GoDoc comments so the SDK renders cleanly on `pkg.go.dev`.

## Available Methods

- `Health(ctx)`
- `CreateTenant(ctx, req)`
- `TenantStats(ctx, tenantID)`
- `StoreMemory(ctx, req)`
- `StoreMemoryBatch(ctx, req)`
- `SearchMemory(ctx, req)`
- `DeleteMemory(ctx, tenantID, memoryID)`

## Memory Payload Notes

`StoreMemoryRequest` supports provenance fields:
- `source` (optional string)
- `created_by` (optional: `auto|user|system`)
- `kind` (optional: `raw_turn|observation|summary|event`, default `raw_turn`)

`SearchMemoryRequest` supports retrieval filters:
- `min_score` (`0..1`)
- `tiers` (`working|episodic|semantic`)
- `kinds` (`raw_turn|observation|summary|event`)
- `disable_touch` (optional bool; skip recency/recall metadata updates for eval-style queries)

`SearchMemoryResponse.Items[*]` includes provenance and recall metadata:
- `source`
- `created_by`
- `kind`
- `recall_count`
- `last_accessed_at`
- `last_recalled_at`
