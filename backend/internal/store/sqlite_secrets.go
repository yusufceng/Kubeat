package store

import (
	"context"
	"fmt"

	"github.com/yusufceng/Kubeat/backend/internal/secretbox"
)

// UseSecrets makes the store encrypt the secrets of monitors and channels
// with box from now on, and able to read those already encrypted. Call it
// before the store is used; nil stores secrets as they are.
func (s *SQLite) UseSecrets(box *secretbox.Box) { s.secrets = box }

// SecretCounts says how the secrets in the database are stored.
type SecretCounts struct {
	// Sealed is the number of encrypted values, Plain of those that are not.
	Sealed, Plain int
}

// rawSecrets visits every secret as it is stored, without decrypting it.
func (s *SQLite) rawSecrets(ctx context.Context, visit func(stored string)) error {
	rows, err := s.read.QueryContext(ctx, "SELECT "+monitorColumns+" FROM monitors")
	if err != nil {
		return err
	}
	var monitors []Monitor
	for rows.Next() {
		r := monitorRow{raw: true}
		if err := rows.Scan(r.dest()...); err != nil {
			rows.Close()
			return err
		}
		m, err := r.monitor()
		if err != nil {
			rows.Close()
			return err
		}
		monitors = append(monitors, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, m := range monitors {
		// Visiting is all that happens; the values are returned unchanged.
		_ = monitorSecrets(&m, func(stored string) (string, error) {
			visit(stored)
			return stored, nil
		})
	}

	channels, err := s.read.QueryContext(ctx, "SELECT "+channelColumns+" FROM notification_channels")
	if err != nil {
		return err
	}
	defer channels.Close()
	for channels.Next() {
		c, err := scanRawChannel(channels)
		if err != nil {
			return err
		}
		visit(c.Target)
		visit(c.Secret)
	}
	return channels.Err()
}

// CountSecrets reports how many secrets are stored encrypted and how many
// are not.
func (s *SQLite) CountSecrets(ctx context.Context) (SecretCounts, error) {
	var counts SecretCounts
	err := s.rawSecrets(ctx, func(stored string) {
		switch {
		case stored == "":
		case secretbox.Sealed(stored):
			counts.Sealed++
		default:
			counts.Plain++
		}
	})
	if err != nil {
		return SecretCounts{}, fmt.Errorf("count secrets: %w", err)
	}
	return counts, nil
}

// EncryptSecrets rewrites every monitor and channel, which stores their
// secrets with the key the store was given. It is how a database written
// without a key comes to be encrypted.
func (s *SQLite) EncryptSecrets(ctx context.Context) error {
	monitors, err := s.ListMonitors(ctx)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}
	for _, m := range monitors {
		if m.HTTP.AuthSecret == "" && m.Database.Password == "" && len(m.HTTP.Headers) == 0 {
			continue
		}
		if _, err := s.UpdateMonitor(ctx, m); err != nil {
			return fmt.Errorf("encrypt secrets: monitor %d: %w", m.ID, err)
		}
	}
	channels, err := s.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("encrypt secrets: %w", err)
	}
	for _, c := range channels {
		if _, err := s.UpdateChannel(ctx, c); err != nil {
			return fmt.Errorf("encrypt secrets: channel %d: %w", c.ID, err)
		}
	}
	return nil
}
