package sqlutil

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	sqlText := `
-- header comment
SELECT 'a;b' AS s, "c;d" AS id;
/* block ; comment */
CREATE TABLE foo (id INT);
-- trailing comment only
`
	got := SplitStatements(sqlText, "sqlite")
	want := []string{
		"-- header comment\nSELECT 'a;b' AS s, \"c;d\" AS id",
		"/* block ; comment */\nCREATE TABLE foo (id INT)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSplitPostgresDollarQuote(t *testing.T) {
	sqlText := `
CREATE FUNCTION hello() RETURNS text AS $$
BEGIN
  RETURN 'x;y';
END;
$$ LANGUAGE plpgsql;
SELECT 1;
`
	got := SplitStatements(sqlText, "postgres")
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %#v", len(got), got)
	}
}

func TestIsReadLikeQuery(t *testing.T) {
	if !IsReadLikeQuery("-- comment\nSELECT * FROM users", false) {
		t.Fatal("expected SELECT to be read-like")
	}
	if !IsReadLikeQuery("INSERT INTO users(name) VALUES ('returning') RETURNING id", false) {
		t.Fatal("expected INSERT ... RETURNING to be read-like")
	}
	if IsReadLikeQuery("INSERT INTO users(name) VALUES ('RETURNING')", false) {
		t.Fatal("expected literal 'RETURNING' not to mark INSERT as read-like")
	}
}
