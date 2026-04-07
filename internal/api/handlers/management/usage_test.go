package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type stubUsagePersister struct {
	persistErr error
	snapshots  []usage.StatisticsSnapshot
}

func (s *stubUsagePersister) PersistUsageEvent(context.Context, usage.UsageEvent) error { return nil }

func (s *stubUsagePersister) PersistUsageSnapshot(_ context.Context, snapshot usage.StatisticsSnapshot) error {
	if s.persistErr != nil {
		return s.persistErr
	}
	s.snapshots = append(s.snapshots, snapshot)
	return nil
}

func (s *stubUsagePersister) LoadUsageEvents(context.Context) ([]usage.UsageEvent, error) { return nil, nil }

func TestImportUsageStatistics_PersistsSnapshotBeforeMerging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usage.SetUsagePersister(nil)
	defer usage.SetUsagePersister(nil)

	persister := &stubUsagePersister{}
	usage.SetUsagePersister(persister)

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	stats := usage.NewRequestStatistics()
	h.SetUsageStatistics(stats)

	payload := usageImportPayload{
		Version: 1,
		Usage: usage.StatisticsSnapshot{
			APIs: map[string]usage.APISnapshot{
				"test-api": {
					Models: map[string]usage.ModelSnapshot{
						"gpt-test": {
							Details: []usage.RequestDetail{
								{
									Timestamp: time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC),
									Source:    "test",
									AuthIndex: "0",
									Tokens: usage.TokenStats{
										InputTokens:  10,
										OutputTokens: 5,
										TotalTokens:  15,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.ImportUsageStatistics(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if len(persister.snapshots) != 1 {
		t.Fatalf("expected 1 persisted snapshot, got %d", len(persister.snapshots))
	}
	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 {
		t.Fatalf("expected merged snapshot total_requests=1, got %d", snapshot.TotalRequests)
	}
}

func TestImportUsageStatistics_ReturnsErrorWhenPersistenceFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usage.SetUsagePersister(nil)
	defer usage.SetUsagePersister(nil)

	persister := &stubUsagePersister{persistErr: errors.New("boom")}
	usage.SetUsagePersister(persister)

	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	stats := usage.NewRequestStatistics()
	h.SetUsageStatistics(stats)

	body := []byte(`{"version":1,"usage":{"apis":{"test-api":{"models":{"gpt-test":{"details":[{"timestamp":"2026-03-21T12:00:00Z","source":"test","auth_index":"0","tokens":{"input_tokens":10,"output_tokens":5,"total_tokens":15},"failed":false}]}}}}}}`)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.ImportUsageStatistics(ctx)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	if got := stats.Snapshot().TotalRequests; got != 0 {
		t.Fatalf("expected in-memory stats to remain unchanged on persistence failure, got total_requests=%d", got)
	}
}
