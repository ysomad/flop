package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func migrate(ctx context.Context, db *pgxpool.Pool) error {
	migrations := []string{
		`DROP TABLE IF EXISTS payments`,

		`CREATE TABLE IF NOT EXISTS payments (
			id uuid NOT NULL,
			amount bigint NOT NULL CHECK (amount >= 0),
			created_at timestamptz NOT NULL,
			captured_at timestamptz NOT NULL,
			provider text NOT NULL,
			user_id text NOT NULL,
			PRIMARY KEY (id, captured_at)
		)`,

		`WITH ids AS MATERIALIZED (
			SELECT bucket, gen_random_uuid() AS id
			FROM generate_series(0, 29999) AS buckets(bucket)
		)
		INSERT INTO payments (id, amount, created_at, captured_at, provider, user_id)
		SELECT
			-- Reuse UUIDs across creation times to exercise the composite key.
			ids.id,
			CASE n % 13
				WHEN 0 THEN 0
				WHEN 1 THEN n % 100
				WHEN 2 THEN 99 + (n % 900)
				WHEN 3 THEN 1000 + (n * 7919) % 99000
				WHEN 4 THEN 100000 + (n * 104729) % 9900000
				WHEN 5 THEN 10000000 + (n * 1299709) % 990000000
				ELSE (n * 15485863) % 25000000
			END,
			timestamptz '2022-01-01 00:00:00+00'
				+ ((n * 7919) % 1461) * interval '1 day'
				+ ((n * 104729) % 86400) * interval '1 second'
				+ n * interval '1 microsecond',
			-- Timestamp ties exercise secondary ordering and cursor seeking.
			timestamptz '2022-01-01 00:00:00+00'
				+ (n / 30000) * interval '1 day'
				+ ((n % 30000) % 600) * interval '1 minute',
			(ARRAY['stripe', 'adyen', 'paypal', 'checkout', 'worldpay', 'manual'])[(1 + n % 6)::int],
			'usr_' || lpad(((n * 3571) % 25000)::text, 5, '0')
		FROM generate_series(1::bigint, 120000::bigint) AS series(n)
		JOIN ids ON ids.bucket = n % 30000
		WHERE NOT EXISTS (SELECT 1 FROM payments)`,
	}
	for i, m := range migrations {
		if _, err := db.Exec(ctx, m); err != nil {
			return fmt.Errorf("migration statement %d: %w", i+1, err)
		}
	}
	return nil
}
