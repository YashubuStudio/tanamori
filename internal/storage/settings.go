package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

func (db *DB) LoadSettings(ctx context.Context) (domain.Settings, error) {
	settings := domain.DefaultSettings()
	var raw string
	err := db.sql.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'classification'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("load classification settings: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return settings, fmt.Errorf("decode classification settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return settings, err
	}
	return settings, nil
}

func (db *DB) SaveSettings(ctx context.Context, settings domain.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES('classification',?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, string(raw), formatTime(time.Now().UTC()))
	return err
}
