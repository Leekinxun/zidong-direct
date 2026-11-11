package db

import (
	"strings"
	"testing"
)

func TestBuildCreateAndAlterDDL(t *testing.T) {
	engines := []EngineType{EngineMySQL, EnginePostgres, EngineSQLite, EngineSQLServer, EngineOracle, EngineDameng}
	for _, eng := range engines {
		d, err := GetDialect(eng)
		if err != nil {
			t.Fatalf("dialect %s missing: %v", eng, err)
		}
		schema := TableSchema{
			Database: "appdb",
			Schema:   "public",
			Table:    "users",
			Columns: []ColumnDef{
				{Name: "id", Type: "INT", Nullable: false, PrimaryKey: true, AutoIncrement: true},
				{Name: "email", Type: "VARCHAR(120)", Nullable: false},
			},
			Indexes: []IndexDef{
				{Name: "idx_users_email", Columns: []string{"email"}, Unique: true},
			},
		}
		preview, err := BuildDDLPreview(d, TableDesignDiff{
			Mode:   "create",
			Target: schema,
		})
		if err != nil {
			t.Fatalf("%s create ddl failed: %v", eng, err)
		}
		if !strings.Contains(preview.SQL, "CREATE TABLE") || !strings.Contains(preview.SQL, "idx_users_email") {
			t.Fatalf("%s unexpected create SQL: %s", eng, preview.SQL)
		}
	}
}

func TestSQLiteRebuildAlter(t *testing.T) {
	d, err := GetDialect(EngineSQLite)
	if err != nil {
		t.Fatal(err)
	}
	orig := TableSchema{
		Database: "main",
		Schema:   "main",
		Table:    "items",
		Columns: []ColumnDef{
			{Name: "id", Type: "INTEGER", PrimaryKey: true, Nullable: false},
			{Name: "title", Type: "TEXT", Nullable: true},
		},
	}
	target := TableSchema{
		Database: "main",
		Schema:   "main",
		Table:    "items",
		Columns: []ColumnDef{
			{Name: "id", OriginalName: "id", Type: "INTEGER", PrimaryKey: true, Nullable: false},
			{Name: "name", OriginalName: "title", Type: "VARCHAR(100)", Nullable: false},
		},
	}
	preview, err := BuildDDLPreview(d, TableDesignDiff{
		Mode:      "alter",
		Database:  "main",
		Schema:    "main",
		TableName: "items",
		Original:  &orig,
		Target:    target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.SQL, "__webdb_tmp_items") || !strings.Contains(preview.SQL, "PRAGMA foreign_keys = OFF") {
		t.Fatalf("expected SQLite table rebuild DDL, got: %s", preview.SQL)
	}
}
