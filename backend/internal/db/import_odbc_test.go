package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuoteODBCSourceTable(t *testing.T) {
	for _, tt := range []struct {
		name, driver, want string
		access             bool
	}{
		{"库存 明细", "", "[库存 明细]", true},
		{"dbo.Orders", "ODBC Driver 17 for SQL Server", "[dbo].[Orders]", false},
		{"sales.订单", "MySQL ODBC Unicode Driver", "`sales`.`订单`", false},
		{"public.orders", "PostgreSQL Unicode", `"public"."orders"`, false},
	} {
		got, err := quoteODBCSourceTable(tt.name, tt.driver, tt.access)
		if err != nil || got != tt.want {
			t.Fatalf("%q: got %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
	for _, name := range []string{"", "dbo..Orders", "db.s.dbo.Orders", "表\x00名", "foo] ; SELECT 1", `"quoted"`, "schema.quoted`"} {
		if _, err := quoteODBCSourceTable(name, "SQL Server", false); err == nil {
			t.Fatalf("accepted invalid table name %q", name)
		}
	}
}

func TestODBCAttributesAreDelimited(t *testing.T) {
	got, err := odbcCredentials("User", "has;special=value")
	if err != nil || got != "UID=User;PWD={has;special=value};" {
		t.Fatalf("unexpected attribute quoting: %q, %v", got, err)
	}
	if _, err := odbcCredentials("User", "invalid};attribute=other"); err == nil || strings.Contains(err.Error(), "invalid}") {
		t.Fatalf("expected sanitized error, got %v", err)
	}
}

func TestAccessImportCopyLimits(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "access.mdb")
	input := strings.NewReader("database")
	if err := copyAccessImport(ctx, input, int64(input.Len()), 4, file); err == nil {
		t.Fatal("accepted a file above the configured payload limit")
	}
	if err := copyAccessImport(ctx, input, 4, 16, file); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil || string(raw) != "data" {
		t.Fatalf("copy did not respect the input section: %q, %v", raw, err)
	}
}
