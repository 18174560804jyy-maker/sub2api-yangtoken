package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type downstreamPricingRepository struct {
	db *sql.DB
}

func NewDownstreamPricingRepository(db *sql.DB) service.DownstreamPricingRepository {
	return &downstreamPricingRepository{db: db}
}

func (r *downstreamPricingRepository) Get(ctx context.Context, channelID int64) (*service.DownstreamPricingConfig, error) {
	config := &service.DownstreamPricingConfig{ChannelID: channelID}
	var overridesJSON, managedJSON, snapshotJSON []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT markup_percent, model_overrides, managed_models,
		       COALESCE(upstream_snapshot, '[]'::jsonb), snapshot_fetched_at
		FROM channel_downstream_pricing_sync WHERE channel_id = $1`, channelID,
	).Scan(&config.MarkupPercent, &overridesJSON, &managedJSON, &snapshotJSON, &config.SnapshotFetchedAt)
	if err == sql.ErrNoRows {
		return config, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get downstream pricing config: %w", err)
	}
	if err := json.Unmarshal(overridesJSON, &config.Overrides); err != nil {
		return nil, fmt.Errorf("decode downstream pricing overrides: %w", err)
	}
	if err := json.Unmarshal(managedJSON, &config.ManagedModels); err != nil {
		return nil, fmt.Errorf("decode managed downstream models: %w", err)
	}
	if err := json.Unmarshal(snapshotJSON, &config.Snapshot); err != nil {
		return nil, fmt.Errorf("decode downstream pricing snapshot: %w", err)
	}
	return config, nil
}

func (r *downstreamPricingRepository) GetLatest(ctx context.Context) (*service.DownstreamPricingConfig, error) {
	config := &service.DownstreamPricingConfig{}
	var overridesJSON, managedJSON, snapshotJSON []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT channel_id, markup_percent, model_overrides, managed_models,
		       COALESCE(upstream_snapshot, '[]'::jsonb), snapshot_fetched_at
		FROM channel_downstream_pricing_sync
		WHERE jsonb_array_length(managed_models) > 0
		ORDER BY updated_at DESC LIMIT 1`).Scan(
		&config.ChannelID, &config.MarkupPercent, &overridesJSON, &managedJSON, &snapshotJSON, &config.SnapshotFetchedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest downstream pricing config: %w", err)
	}
	if err := json.Unmarshal(overridesJSON, &config.Overrides); err != nil {
		return nil, fmt.Errorf("decode latest downstream pricing overrides: %w", err)
	}
	if err := json.Unmarshal(managedJSON, &config.ManagedModels); err != nil {
		return nil, fmt.Errorf("decode latest managed downstream models: %w", err)
	}
	if err := json.Unmarshal(snapshotJSON, &config.Snapshot); err != nil {
		return nil, fmt.Errorf("decode latest downstream pricing snapshot: %w", err)
	}
	return config, nil
}

func (r *downstreamPricingRepository) SaveSnapshot(ctx context.Context, channelID int64, snapshot []service.DownstreamUpstreamModel, fetchedAt time.Time) error {
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode downstream pricing snapshot: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO channel_downstream_pricing_sync (channel_id, upstream_snapshot, snapshot_fetched_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (channel_id) DO UPDATE SET
			upstream_snapshot = EXCLUDED.upstream_snapshot,
			snapshot_fetched_at = EXCLUDED.snapshot_fetched_at,
			updated_at = NOW()`, channelID, string(snapshotJSON), fetchedAt)
	if err != nil {
		return fmt.Errorf("save downstream pricing snapshot: %w", err)
	}
	return nil
}

func (r *downstreamPricingRepository) SyncChannel(ctx context.Context, commit service.DownstreamPricingSyncCommit) error {
	overridesJSON, err := json.Marshal(commit.Overrides)
	if err != nil {
		return fmt.Errorf("encode downstream pricing overrides: %w", err)
	}
	managedJSON, err := json.Marshal(commit.ManagedModels)
	if err != nil {
		return fmt.Errorf("encode managed downstream models: %w", err)
	}
	snapshotJSON, err := json.Marshal(commit.Snapshot)
	if err != nil {
		return fmt.Errorf("encode downstream pricing snapshot: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin downstream pricing sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var channelID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE id = $1 FOR UPDATE`, commit.ChannelID).Scan(&channelID); err != nil {
		if err == sql.ErrNoRows {
			return service.ErrChannelNotFound
		}
		return fmt.Errorf("lock downstream pricing channel: %w", err)
	}

	previousManaged := []string{}
	var previousManagedJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT managed_models FROM channel_downstream_pricing_sync WHERE channel_id = $1 FOR UPDATE`, commit.ChannelID).Scan(&previousManagedJSON)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("lock downstream pricing config: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(previousManagedJSON, &previousManaged); err != nil {
			return fmt.Errorf("decode previous managed models: %w", err)
		}
	}

	existing, err := listModelPricingExec(ctx, tx, commit.ChannelID)
	if err != nil {
		return err
	}
	merged := mergeManagedPricing(existing, previousManaged, commit.ManagedModels, commit.Pricing)

	if _, err := tx.ExecContext(ctx, `UPDATE channels SET restrict_models = TRUE, updated_at = NOW() WHERE id = $1`, commit.ChannelID); err != nil {
		return fmt.Errorf("enable channel model restriction: %w", err)
	}
	if err := replaceModelPricingTx(ctx, tx, commit.ChannelID, merged); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO channel_downstream_pricing_sync
			(channel_id, markup_percent, model_overrides, managed_models, upstream_snapshot, snapshot_fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (channel_id) DO UPDATE SET
			markup_percent = EXCLUDED.markup_percent,
			model_overrides = EXCLUDED.model_overrides,
			managed_models = EXCLUDED.managed_models,
			upstream_snapshot = EXCLUDED.upstream_snapshot,
			snapshot_fetched_at = EXCLUDED.snapshot_fetched_at,
			updated_at = NOW()`, commit.ChannelID, commit.MarkupPercent, string(overridesJSON), string(managedJSON), string(snapshotJSON), commit.FetchedAt); err != nil {
		return fmt.Errorf("save downstream pricing config: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit downstream pricing sync: %w", err)
	}
	return nil
}

func mergeManagedPricing(existing []service.ChannelModelPricing, previousManaged, currentManaged []string, replacement []service.ChannelModelPricing) []service.ChannelModelPricing {
	managedSet := make(map[string]struct{}, len(previousManaged)+len(currentManaged))
	for _, model := range previousManaged {
		managedSet[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	for _, model := range currentManaged {
		managedSet[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
	}
	merged := make([]service.ChannelModelPricing, 0, len(existing)+len(replacement))
	for _, entry := range existing {
		kept := make([]string, 0, len(entry.Models))
		for _, model := range entry.Models {
			if _, managed := managedSet[strings.ToLower(strings.TrimSpace(model))]; !managed {
				kept = append(kept, model)
			}
		}
		if len(kept) == 0 {
			continue
		}
		entry.Models = kept
		merged = append(merged, entry)
	}
	return append(merged, replacement...)
}
