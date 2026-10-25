# Go Client SDK (`pkg/client`)

This document describes the Go client in `pkg/client` for calling Pali API endpoints.

Pali is still very early in development and is not yet a complete memory solution.
Current focus is infrastructure correctness and reliability first.

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

	"github.com/pali-mem/pali/pkg/client"
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

	"github.com/pali-mem/pali/pkg/client"
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
- `ListTenants(ctx, limit)`
- `TenantStats(ctx, tenantID)`
- `StoreMemory(ctx, req)`
- `StoreMemoryBatch(ctx, req)`
- `IngestMemory(ctx, req)`
- `IngestMemoryBatch(ctx, req)`
- `SearchMemory(ctx, req)`
- `ListPostprocessJobs(ctx, req)`
- `GetPostprocessJob(ctx, jobID)`
- `GetMemory(ctx, tenantID, memoryID)`
- `ListMemory(ctx, req)`
- `UpdateMemory(ctx, tenantID, memoryID, req)`
- `DeleteMemory(ctx, tenantID, memoryID)`

## Get, List, And Update

```go
// Page through a tenant's memories, newest first.
req := client.ListMemoryRequest{TenantID: "tenant_1", Limit: 100}
for {
    page, err := c.ListMemory(ctx, req)
    if err != nil {
        return err
    }
    for _, m := range page.Items {
        fmt.Println(m.ID, m.Content)
    }
    if page.NextOffset == nil {
        break
    }
    req.Offset = *page.NextOffset
}

// Fetch one memory. Unknown IDs and IDs from another tenant return client.ErrNotFound.
m, err := c.GetMemory(ctx, "tenant_1", "mem_abc123")

// Update any subset of content, tags, tier, and importance. Nil fields are unchanged.
content := "User prefers green tea."
tags := []string{"preferences"}
updated, err := c.UpdateMemory(ctx, "tenant_1", m.ID, client.UpdateMemoryRequest{
    Content: &content,
    Tags:    &tags,
})
```

Notes:
- `ListMemoryRequest` supports `Limit` (default 50, max 200), `Offset`, `Tiers`, and `Kinds`. `ListMemoryResponse.NextOffset` is nil on the last page.
- `UpdateMemoryRequest` with no fields set returns a `400` `*APIError`. A non-nil empty `Tags` slice clears the tags.
- `ListTenants` returns each tenant with `MemoryCount`; a tenant-scoped token only sees its own tenant.
- `TenantStatsResponse` carries `MemoryCount` plus `ByTier`, `ByKind`, `TotalRecalls`, and `NewestMemoryAt` (nil for an empty tenant).
- `GetMemory`, `ListMemory`, and `UpdateMemory` are methods on `*Client` and are not part of the `MemoryClient` interface, so existing mocks of that interface keep compiling.

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
- `importance`
- `recall_count`
- `last_accessed_at`
- `last_recalled_at`
