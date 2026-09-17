package migrations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestAccessTokenExpiryMigrationPostgres(t *testing.T) {
	dsn := os.Getenv("SILO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SILO_TEST_DATABASE_URL is not set")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = db.Close() })
	const file = "20260917000328_raise_default_access_token_expiry.sql"
	data, err := FS.ReadFile("sql/" + file)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1h", "8h", "24h", "12h", "48h", "60m", "", "missing"} {
		t.Run(value, func(t *testing.T) {
			schema := fmt.Sprintf("auth_expiry_%d", time.Now().UnixNano())
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + schema)
			t.Cleanup(func() {
				if _, err := db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
					t.Error(err)
				}
			})
			exec("CREATE TABLE " + schema + ".server_settings (key text PRIMARY KEY, value text NOT NULL)")
			exec("INSERT INTO " + schema + ".server_settings VALUES ('auth.refresh_token_expiry', '30d'), ('unrelated', '8h')")
			if value != "missing" {
				exec("INSERT INTO "+schema+".server_settings VALUES ('auth.access_token_expiry', $1)", value)
			}
			fixture := fstest.MapFS{file: &fstest.MapFile{Data: []byte(strings.ReplaceAll(string(data), "public.", schema+"."))}}
			provider, err := goose.NewProvider(goose.DialectPostgres, db, fixture, goose.WithTableName(schema+".goose_db_version"))
			if err != nil {
				t.Fatal(err)
			}
			want := value
			if value == "1h" || value == "8h" {
				want = "24h"
			}
			check := func() {
				t.Helper()
				var got string
				if err := db.QueryRowContext(t.Context(), "SELECT COALESCE((SELECT value FROM "+schema+".server_settings WHERE key = 'auth.access_token_expiry'), 'missing')").Scan(&got); err != nil || got != want {
					t.Fatalf("access expiry = %q, want %q: %v", got, want, err)
				}
				var unchanged int
				if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+schema+".server_settings WHERE (key = 'auth.refresh_token_expiry' AND value = '30d') OR (key = 'unrelated' AND value = '8h')").Scan(&unchanged); err != nil || unchanged != 2 {
					t.Fatalf("unrelated settings changed: count=%d: %v", unchanged, err)
				}
			}
			for range 2 {
				if _, err := provider.Up(t.Context()); err != nil {
					t.Fatal(err)
				}
				check()
			}
			if _, err := provider.Down(t.Context()); err != nil {
				t.Fatal(err)
			}
			check()
		})
	}
}
