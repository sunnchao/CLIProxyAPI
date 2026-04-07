package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// ImportLegacyLocalIfNeeded seeds PostgreSQL from the pre-Postgres local workspace when needed.
func (s *PostgresStore) ImportLegacyLocalIfNeeded(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres store: not initialized")
	}
	if err := s.importLegacyConfigIfNeeded(ctx); err != nil {
		return err
	}
	if err := s.importLegacyAuthIfNeeded(ctx); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) importLegacyConfigIfNeeded(ctx context.Context) error {
	exists, err := s.configRecordExists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	data, ok, err := readLegacyConfigSeed(s.cfg.LegacyConfigPath)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := s.persistConfig(ctx, data); err != nil {
		return err
	}
	log.Infof("postgres store: imported legacy config from %s", s.cfg.LegacyConfigPath)
	return nil
}

func (s *PostgresStore) importLegacyAuthIfNeeded(ctx context.Context) error {
	legacyDir := strings.TrimSpace(s.cfg.LegacyAuthDir)
	if legacyDir == "" {
		return nil
	}
	absLegacyDir, err := filepath.Abs(legacyDir)
	if err != nil {
		return fmt.Errorf("postgres store: resolve legacy auth dir: %w", err)
	}
	if filepath.Clean(absLegacyDir) == filepath.Clean(s.authDir) {
		return nil
	}
	info, err := os.Stat(absLegacyDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("postgres store: inspect legacy auth dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("postgres store: legacy auth dir %s is not a directory", absLegacyDir)
	}
	return filepath.WalkDir(absLegacyDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("postgres store: walk legacy auth dir: %w", walkErr)
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			return nil
		}
		data, errRead := os.ReadFile(path)
		if errRead != nil {
			return fmt.Errorf("postgres store: read legacy auth %s: %w", path, errRead)
		}
		var payload map[string]any
		if errJSON := json.Unmarshal(data, &payload); errJSON != nil {
			return fmt.Errorf("postgres store: invalid legacy auth %s: %w", path, errJSON)
		}
		relID, errRel := filepath.Rel(absLegacyDir, path)
		if errRel != nil {
			return fmt.Errorf("postgres store: compute legacy auth path: %w", errRel)
		}
		relID = filepath.ToSlash(filepath.Clean(relID))
		if strings.HasPrefix(relID, "..") || relID == "." || relID == "" {
			return fmt.Errorf("postgres store: legacy auth path %s escapes base directory", path)
		}
		inserted, errInsert := s.insertAuthIfMissing(ctx, relID, data)
		if errInsert != nil {
			return errInsert
		}
		if inserted {
			log.Infof("postgres store: imported legacy auth %s", relID)
		}
		return nil
	})
}

func (s *PostgresStore) configRecordExists(ctx context.Context) (bool, error) {
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE id = $1", s.fullTableName(s.cfg.ConfigTable))
	var exists int
	err := s.db.QueryRowContext(ctx, query, defaultConfigKey).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("postgres store: check config record: %w", err)
	}
	return true, nil
}

func (s *PostgresStore) insertAuthIfMissing(ctx context.Context, relID string, data []byte) (bool, error) {
	query := fmt.Sprintf(`
		INSERT INTO %s (id, content, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, s.fullTableName(s.cfg.AuthTable))
	result, err := s.db.ExecContext(ctx, query, relID, json.RawMessage(data))
	if err != nil {
		return false, fmt.Errorf("postgres store: import legacy auth %s: %w", relID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return true, nil
	}
	return rowsAffected > 0, nil
}

func readLegacyConfigSeed(path string) ([]byte, bool, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(trimmed)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("postgres store: read legacy config %s: %w", trimmed, err)
	}
	var parsed any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, false, fmt.Errorf("postgres store: parse legacy config %s: %w", trimmed, err)
	}
	return data, true, nil
}

// PersistUsageEvent stores a normalized usage event in PostgreSQL.
func (s *PostgresStore) PersistUsageEvent(ctx context.Context, event usage.UsageEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres store: not initialized")
	}
	event = usage.NewUsageEventFromSnapshot(event.APIName, event.Model, event.Detail())
	query := fmt.Sprintf(`
		INSERT INTO %s (
			event_key,
			requested_at,
			api_name,
			model,
			source,
			auth_index,
			failed,
			input_tokens,
			output_tokens,
			reasoning_tokens,
			cached_tokens,
			total_tokens,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		ON CONFLICT (event_key) DO NOTHING
	`, s.fullTableName(s.cfg.UsageTable))
	_, err := s.db.ExecContext(
		ctx,
		query,
		event.EventKey,
		event.RequestedAt,
		event.APIName,
		event.Model,
		event.Source,
		event.AuthIndex,
		event.Failed,
		event.Tokens.InputTokens,
		event.Tokens.OutputTokens,
		event.Tokens.ReasoningTokens,
		event.Tokens.CachedTokens,
		event.Tokens.TotalTokens,
	)
	if err != nil {
		return fmt.Errorf("postgres store: persist usage event: %w", err)
	}
	return nil
}

// PersistUsageSnapshot stores snapshot details as normalized usage events.
func (s *PostgresStore) PersistUsageSnapshot(ctx context.Context, snapshot usage.StatisticsSnapshot) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres store: not initialized")
	}
	for apiName, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				event := usage.NewUsageEventFromSnapshot(apiName, modelName, detail)
				if err := s.PersistUsageEvent(ctx, event); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// LoadUsageEvents returns all persisted usage events in timestamp order.
func (s *PostgresStore) LoadUsageEvents(ctx context.Context) ([]usage.UsageEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres store: not initialized")
	}
	query := fmt.Sprintf(`
		SELECT
			event_key,
			requested_at,
			api_name,
			model,
			source,
			auth_index,
			failed,
			input_tokens,
			output_tokens,
			reasoning_tokens,
			cached_tokens,
			total_tokens
		FROM %s
		ORDER BY requested_at, event_key
	`, s.fullTableName(s.cfg.UsageTable))
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres store: load usage events: %w", err)
	}
	defer rows.Close()

	events := make([]usage.UsageEvent, 0, 128)
	for rows.Next() {
		var (
			eventKey        string
			requestedAt     time.Time
			apiName         string
			modelName       string
			source          string
			authIndex       string
			failed          bool
			inputTokens     int64
			outputTokens    int64
			reasoningTokens int64
			cachedTokens    int64
			totalTokens     int64
		)
		if err := rows.Scan(
			&eventKey,
			&requestedAt,
			&apiName,
			&modelName,
			&source,
			&authIndex,
			&failed,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cachedTokens,
			&totalTokens,
		); err != nil {
			return nil, fmt.Errorf("postgres store: scan usage event: %w", err)
		}
		event := usage.NewUsageEventFromSnapshot(apiName, modelName, usage.RequestDetail{
			Timestamp: requestedAt,
			Source:    source,
			AuthIndex: authIndex,
			Tokens: usage.TokenStats{
				InputTokens:     inputTokens,
				OutputTokens:    outputTokens,
				ReasoningTokens: reasoningTokens,
				CachedTokens:    cachedTokens,
				TotalTokens:     totalTokens,
			},
			Failed: failed,
		})
		if strings.TrimSpace(eventKey) != "" {
			event.EventKey = eventKey
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres store: iterate usage events: %w", err)
	}
	return events, nil
}
