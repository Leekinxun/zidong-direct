package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	go_ora "github.com/sijms/go-ora/v2"
)

type oracleDialect struct{}

func init() {
	RegisterDialect(&oracleDialect{})
}

func (d *oracleDialect) Engine() EngineType { return EngineOracle }
func (d *oracleDialect) DriverName() string { return "oracle" }

func (d *oracleDialect) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		Engine:              EngineOracle,
		DefaultPort:         1521,
		SupportsSchemas:     true,
		SupportsSSH:         true,
		SupportsAlterColumn: true,
		SupportsForeignKeys: true,
		DataTypes: []string{
			"NUMBER(10)", "NUMBER(19)", "NUMBER(18,2)", "NUMBER",
			"VARCHAR2(255)", "VARCHAR2(100)", "NVARCHAR2(255)", "CHAR(36)", "CLOB",
			"DATE", "TIMESTAMP", "TIMESTAMP WITH TIME ZONE",
			"BLOB", "RAW(16)",
		},
	}
}

func (d *oracleDialect) QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (d *oracleDialect) Placeholder(index int) string {
	return fmt.Sprintf(":%d", index)
}

func (d *oracleDialect) BuildDSN(cfg ConnectionConfig, targetHost string, targetPort int, databaseOverride string) (string, error) {
	serviceOrSID := cfg.Database
	if databaseOverride != "" {
		serviceOrSID = databaseOverride
	}
	opts := map[string]string{
		"TIMEOUT": "10",
	}
	if strings.EqualFold(cfg.SSLMode, "require") || strings.EqualFold(cfg.SSLMode, "verify-full") {
		opts["SSL"] = "enable"
		if cfg.TrustServerCer {
			opts["SSL VERIFY"] = "false"
		}
	}
	if strings.EqualFold(cfg.OracleMode, "sid") {
		opts["SID"] = serviceOrSID
		return go_ora.BuildUrl(targetHost, targetPort, "", cfg.Username, cfg.Password, opts), nil
	}
	return go_ora.BuildUrl(targetHost, targetPort, serviceOrSID, cfg.Username, cfg.Password, opts), nil
}

func (d *oracleDialect) ListDatabases(_ context.Context, _ *sql.DB, cfg ConnectionConfig) ([]string, error) {
	if cfg.Database != "" {
		return []string{cfg.Database}, nil
	}
	return []string{"ORCL"}, nil
}

func (d *oracleDialect) ListSchemas(ctx context.Context, conn *sql.DB, cfg ConnectionConfig, _ string) ([]string, error) {
	q := `
SELECT username
FROM all_users
ORDER BY CASE WHEN UPPER(username) = UPPER(:1) THEN 0 ELSE 1 END, username`
	rows, err := conn.QueryContext(ctx, q, cfg.Username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (d *oracleDialect) ListObjects(ctx context.Context, conn *sql.DB, _, schema string) ([]DatabaseObject, error) {
	if schema == "" {
		_ = conn.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&schema)
	}
	q := `
SELECT t.table_name, 'TABLE', t.num_rows, c.comments
FROM all_tables t
LEFT JOIN all_tab_comments c ON c.owner = t.owner AND c.table_name = t.table_name
WHERE t.owner = :1
UNION ALL
SELECT v.view_name, 'VIEW', NULL, c.comments
FROM all_views v
LEFT JOIN all_tab_comments c ON c.owner = v.owner AND c.table_name = v.view_name
WHERE v.owner = :2
ORDER BY 2, 1`
	rows, err := conn.QueryContext(ctx, q, schema, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DatabaseObject
	for rows.Next() {
		var name, objType string
		var numRows sql.NullInt64
		var comment sql.NullString
		if err := rows.Scan(&name, &objType, &numRows, &comment); err != nil {
			return nil, err
		}
		var estPtr *int64
		if numRows.Valid {
			v := numRows.Int64
			estPtr = &v
		}
		out = append(out, DatabaseObject{
			Name:        name,
			Type:        objType,
			Schema:      schema,
			RowEstimate: estPtr,
			Comment:     comment.String,
		})
	}
	return out, rows.Err()
}

func (d *oracleDialect) DescribeTable(ctx context.Context, conn *sql.DB, database, schema, table string) (TableSchema, error) {
	if schema == "" {
		_ = conn.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&schema)
	}
	ts := TableSchema{
		Database: database,
		Schema:   schema,
		Table:    table,
	}

	colQuery := `
SELECT c.column_name,
       CASE
         WHEN c.data_type IN ('VARCHAR2', 'NVARCHAR2', 'CHAR', 'NCHAR', 'RAW') THEN
           c.data_type || '(' || c.char_length || ')'
         WHEN c.data_type = 'NUMBER' AND c.data_precision IS NOT NULL THEN
           CASE WHEN COALESCE(c.data_scale, 0) > 0
             THEN 'NUMBER(' || c.data_precision || ',' || c.data_scale || ')'
             ELSE 'NUMBER(' || c.data_precision || ')'
           END
         ELSE c.data_type
       END AS col_type,
       c.nullable,
       c.data_default,
       CASE WHEN pk.column_name IS NOT NULL THEN 1 ELSE 0 END AS is_pk,
       COALESCE(cc.comments, ''),
       c.column_id
FROM all_tab_columns c
LEFT JOIN all_col_comments cc
  ON cc.owner = c.owner AND cc.table_name = c.table_name AND cc.column_name = c.column_name
LEFT JOIN (
  SELECT acc.owner, acc.table_name, acc.column_name
  FROM all_constraints ac
  JOIN all_cons_columns acc ON acc.owner = ac.owner AND acc.constraint_name = ac.constraint_name
  WHERE ac.constraint_type = 'P'
) pk ON pk.owner = c.owner AND pk.table_name = c.table_name AND pk.column_name = c.column_name
WHERE c.owner = :1 AND c.table_name = :2
ORDER BY c.column_id`
	rows, err := conn.QueryContext(ctx, colQuery, schema, table)
	if err != nil {
		return ts, err
	}
	for rows.Next() {
		var col ColumnDef
		var nullable string
		var defVal sql.NullString
		var isPK int
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &defVal, &isPK, &col.Comment, &col.Ordinal); err != nil {
			_ = rows.Close()
			return ts, err
		}
		col.Nullable = strings.EqualFold(nullable, "Y")
		col.PrimaryKey = isPK == 1
		if defVal.Valid {
			trimmed := strings.TrimSpace(defVal.String)
			col.DefaultValue = &trimmed
		}
		ts.Columns = append(ts.Columns, col)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return ts, err
	}
	_ = rows.Close()
	if len(ts.Columns) == 0 {
		return ts, fmt.Errorf("table %s.%s not found", schema, table)
	}

	idxQuery := `
SELECT i.index_name, i.uniqueness, ic.column_name
FROM all_indexes i
JOIN all_ind_columns ic ON ic.index_owner = i.owner AND ic.index_name = i.index_name
WHERE i.table_owner = :1 AND i.table_name = :2
ORDER BY i.index_name, ic.column_position`
	idxRows, err := conn.QueryContext(ctx, idxQuery, schema, table)
	if err == nil {
		idxMap := map[string]*IndexDef{}
		var idxOrder []string
		for idxRows.Next() {
			var name, uniq, colName string
			if scanErr := idxRows.Scan(&name, &uniq, &colName); scanErr == nil {
				entry, ok := idxMap[name]
				if !ok {
					entry = &IndexDef{
						Name:   name,
						Unique: strings.EqualFold(uniq, "UNIQUE"),
					}
					idxMap[name] = entry
					idxOrder = append(idxOrder, name)
				}
				entry.Columns = append(entry.Columns, colName)
			}
		}
		_ = idxRows.Close()
		for _, name := range idxOrder {
			ts.Indexes = append(ts.Indexes, *idxMap[name])
		}
	}

	fkQuery := `
SELECT ac.constraint_name,
       acc.column_name,
       rac.owner,
       rac.table_name,
       racc.column_name,
       COALESCE(ac.delete_rule, '')
FROM all_constraints ac
JOIN all_cons_columns acc ON acc.owner = ac.owner AND acc.constraint_name = ac.constraint_name
JOIN all_constraints rac ON rac.owner = ac.r_owner AND rac.constraint_name = ac.r_constraint_name
JOIN all_cons_columns racc ON racc.owner = rac.owner AND racc.constraint_name = rac.constraint_name AND racc.position = acc.position
WHERE ac.constraint_type = 'R' AND ac.owner = :1 AND ac.table_name = :2
ORDER BY ac.constraint_name, acc.position`
	fkRows, err := conn.QueryContext(ctx, fkQuery, schema, table)
	if err == nil {
		fkMap := map[string]*ForeignKeyDef{}
		var fkOrder []string
		for fkRows.Next() {
			var name, col, refSchema, refTable, refCol, delRule string
			if scanErr := fkRows.Scan(&name, &col, &refSchema, &refTable, &refCol, &delRule); scanErr == nil {
				fk, ok := fkMap[name]
				if !ok {
					fk = &ForeignKeyDef{
						Name:      name,
						RefSchema: refSchema,
						RefTable:  refTable,
						OnDelete:  delRule,
					}
					fkMap[name] = fk
					fkOrder = append(fkOrder, name)
				}
				fk.Columns = append(fk.Columns, col)
				fk.RefColumns = append(fk.RefColumns, refCol)
			}
		}
		_ = fkRows.Close()
		for _, name := range fkOrder {
			ts.ForeignKeys = append(ts.ForeignKeys, *fkMap[name])
		}
	}

	ts.DDL = BuildCreateTableSQL(d, ts)
	return ts, nil
}

func (d *oracleDialect) PaginateSelect(baseSelect string, orderBy string, limit, offset, startParamIdx int) (string, []any) {
	var inner strings.Builder
	inner.WriteString(baseSelect)
	if orderBy != "" {
		inner.WriteString(" ORDER BY ")
		inner.WriteString(orderBy)
	}
	maxRow := offset + limit
	q := fmt.Sprintf(
		"SELECT * FROM (SELECT rea_inner.*, ROWNUM rea_rn FROM (%s) rea_inner WHERE ROWNUM <= %s) WHERE rea_rn > %s",
		inner.String(),
		d.Placeholder(startParamIdx),
		d.Placeholder(startParamIdx+1),
	)
	return q, []any{maxRow, offset}
}
