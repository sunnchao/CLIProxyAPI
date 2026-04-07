package usage

import (
	"context"
	"strings"
	"sync"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

// UsageEvent is the normalized, deduplicated representation persisted for one request.
type UsageEvent struct {
	EventKey    string
	RequestedAt time.Time
	APIName     string
	Model       string
	Source      string
	AuthIndex   string
	Failed      bool
	Tokens      TokenStats
}

// UsagePersister persists normalized usage events to durable storage.
type UsagePersister interface {
	PersistUsageEvent(ctx context.Context, event UsageEvent) error
	PersistUsageSnapshot(ctx context.Context, snapshot StatisticsSnapshot) error
	LoadUsageEvents(ctx context.Context) ([]UsageEvent, error)
}

var (
	usagePersisterMu sync.RWMutex
	usagePersister   UsagePersister
)

// SetUsagePersister configures the global durable usage sink.
func SetUsagePersister(p UsagePersister) {
	usagePersisterMu.Lock()
	usagePersister = p
	usagePersisterMu.Unlock()
}

// GetUsagePersister returns the currently configured durable usage sink.
func GetUsagePersister() UsagePersister {
	usagePersisterMu.RLock()
	p := usagePersister
	usagePersisterMu.RUnlock()
	return p
}

// PersistSnapshot writes the snapshot to the configured durable store when one is available.
func PersistSnapshot(ctx context.Context, snapshot StatisticsSnapshot) error {
	p := GetUsagePersister()
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return p.PersistUsageSnapshot(ctx, snapshot)
}

// RestorePersistedStatistics rebuilds the provided in-memory store from durable events.
func RestorePersistedStatistics(ctx context.Context, stats *RequestStatistics) error {
	p := GetUsagePersister()
	if p == nil || stats == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	events, err := p.LoadUsageEvents(ctx)
	if err != nil {
		return err
	}
	stats.Reset()
	for _, event := range events {
		stats.RecordEvent(event)
	}
	return nil
}

// NewUsageEventFromRecord normalizes a runtime usage record for aggregation and persistence.
func NewUsageEventFromRecord(ctx context.Context, record coreusage.Record) UsageEvent {
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}
	apiName := strings.TrimSpace(record.APIKey)
	if apiName == "" {
		apiName = resolveAPIIdentifier(ctx, record)
	}
	return newUsageEvent(
		apiName,
		record.Model,
		RequestDetail{
			Timestamp: timestamp,
			Source:    record.Source,
			AuthIndex: record.AuthIndex,
			Tokens:    normaliseDetail(record.Detail),
			Failed:    failed,
		},
	)
}

// NewUsageEventFromSnapshot normalizes snapshot details for persistence and deduplication.
func NewUsageEventFromSnapshot(apiName, model string, detail RequestDetail) UsageEvent {
	return newUsageEvent(apiName, model, detail)
}

func newUsageEvent(apiName, model string, detail RequestDetail) UsageEvent {
	apiName = strings.TrimSpace(apiName)
	if apiName == "" {
		apiName = "unknown"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	if detail.Timestamp.IsZero() {
		detail.Timestamp = time.Now()
	}
	detail.Source = strings.TrimSpace(detail.Source)
	detail.AuthIndex = strings.TrimSpace(detail.AuthIndex)
	detail.Tokens = normaliseTokenStats(detail.Tokens)
	event := UsageEvent{
		RequestedAt: detail.Timestamp,
		APIName:     apiName,
		Model:       model,
		Source:      detail.Source,
		AuthIndex:   detail.AuthIndex,
		Failed:      detail.Failed,
		Tokens:      detail.Tokens,
	}
	event.EventKey = dedupKey(event.APIName, event.Model, RequestDetail{
		Timestamp: event.RequestedAt,
		Source:    event.Source,
		AuthIndex: event.AuthIndex,
		Tokens:    event.Tokens,
		Failed:    event.Failed,
	})
	return event
}

// Detail converts the normalized event into the in-memory detail representation.
func (e UsageEvent) Detail() RequestDetail {
	return RequestDetail{
		Timestamp: e.RequestedAt,
		Source:    e.Source,
		AuthIndex: e.AuthIndex,
		Tokens:    normaliseTokenStats(e.Tokens),
		Failed:    e.Failed,
	}
}
