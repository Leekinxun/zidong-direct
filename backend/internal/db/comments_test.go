package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestDatabaseCommentSupport(t *testing.T) {
	ctx := context.Background()
	conn := openCommentFixture(t, `
ATTACH DATABASE ':memory:' AS information_schema;
CREATE TABLE information_schema.COLUMNS (TABLE_SCHEMA TEXT, TABLE_NAME TEXT, COLUMN_NAME TEXT);
CREATE TABLE information_schema.SCHEMATA (SCHEMA_NAME TEXT, SCHEMA_COMMENT TEXT);
INSERT INTO information_schema.SCHEMATA VALUES ('example', '数据库说明');
`)

	details, err := ReadDatabaseDetails(ctx, conn, EngineMySQL, "example")
	if err != nil || details.Name != "example" || details.CommentSupported {
		t.Fatalf("MySQL metadata without comment column: %+v, %v", details, err)
	}
	_, err = conn.Exec(`INSERT INTO information_schema.COLUMNS VALUES ('information_schema', 'SCHEMATA', 'SCHEMA_COMMENT')`)
	if err != nil {
		t.Fatal(err)
	}
	details, err = ReadDatabaseDetails(ctx, conn, EngineMySQL, "example")
	if err != nil || !details.CommentSupported || details.Comment != "数据库说明" {
		t.Fatalf("MariaDB metadata with comment column: %+v, %v", details, err)
	}
	details, err = ReadDatabaseDetails(ctx, conn, EngineSQLite, "main")
	if err != nil || details.Name != "main" || details.CommentSupported {
		t.Fatalf("SQLite has no catalog comments: %+v, %v", details, err)
	}
}

func TestDamengObjectCommentsAreScopedByOwner(t *testing.T) {
	ctx := context.Background()
	conn := openCommentFixture(t, `
CREATE TABLE all_tables (owner TEXT, table_name TEXT, num_rows INT);
CREATE TABLE all_views (owner TEXT, view_name TEXT);
CREATE TABLE all_tab_comments (owner TEXT, table_name TEXT, comments TEXT);
INSERT INTO all_tables VALUES ('OWNER_A', 'ORDERS', 12), ('OWNER_B', 'ORDERS', 20);
INSERT INTO all_views VALUES ('OWNER_A', 'RECENT_ORDERS');
INSERT INTO all_tab_comments VALUES
  ('OWNER_B', 'ORDERS', 'wrong owner'),
  ('OWNER_A', 'ORDERS', '正确的表注释'),
  ('OWNER_A', 'RECENT_ORDERS', '视图注释');
`)
	dialect, err := GetDialect(EngineDameng)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := dialect.ListObjects(ctx, conn, "fixture", "OWNER_A")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 2 || objects[0].Comment != "正确的表注释" || objects[1].Comment != "视图注释" {
		t.Fatalf("unexpected objects: %+v", objects)
	}
	comment, err := ReadTableComment(ctx, conn, EngineDameng, TableSchema{Schema: "OWNER_A", Table: "ORDERS"})
	if err != nil || comment == nil || *comment != "正确的表注释" {
		t.Fatalf("table comment lookup failed: %v, %v", comment, err)
	}
}

func openCommentFixture(t *testing.T, setup string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(setup); err != nil {
		t.Fatal(err)
	}
	return conn
}
