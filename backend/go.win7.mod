// Build Windows 7 binaries with Go 1.20.14 and -modfile=go.win7.mod.
module web-db-client

go 1.20

require (
	gitee.com/chunanyong/dm v1.8.23
	github.com/alexbrainman/odbc v0.0.0-20250601004241-49e6b2bc0cf0
	github.com/andybalholm/brotli v1.0.5
	github.com/go-sql-driver/mysql v1.8.1
	github.com/golang/snappy v0.0.4
	github.com/jackc/pgx/v5 v5.6.0
	github.com/klauspost/compress v1.16.7
	github.com/microsoft/go-mssqldb v1.8.2
	github.com/parquet-go/parquet-go v0.21.0
	github.com/pierrec/lz4/v4 v4.1.18
	github.com/segmentio/encoding v0.3.6
	github.com/shakinm/xlsReader v0.9.12
	github.com/sijms/go-ora/v2 v2.9.0
	golang.org/x/crypto v0.33.0
	golang.org/x/sys v0.30.0
	golang.org/x/text v0.22.0
	// v1.32.0 and later use Go 1.21 APIs such as runtime.Pinner.
	modernc.org/sqlite v1.31.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/metakeule/fmtdate v1.1.2 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sync v0.11.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
