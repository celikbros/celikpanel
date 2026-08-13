package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const serviceOperationSnapshotEquivalenceTimeout = 2 * time.Minute

type serviceOperationLogicalTable struct {
	name         string
	withoutRowID bool
}

type serviceOperationLogicalColumn struct {
	name       string
	expression string
}

type serviceOperationLogicalCell struct {
	storageClass string
	valueHex     sql.NullString
}

func validatePreLedgerSnapshotEquivalenceRequest(
	snapshotPath string,
	snapshotSchemaValue string,
	transaction serviceOperationReleaseTransaction,
	conflictingDatabaseMode bool,
) (bool, error) {
	if strings.TrimSpace(snapshotPath) == "" {
		return false, nil
	}
	if strings.TrimSpace(snapshotPath) != snapshotPath {
		return true, fmt.Errorf("pre-ledger snapshot equivalence path must not contain surrounding whitespace")
	}
	if strings.TrimSpace(snapshotSchemaValue) != "" {
		return true, fmt.Errorf("pre-ledger snapshot equivalence has a fixed pre-ledger schema and must not use --snapshot-schema")
	}
	if conflictingDatabaseMode {
		return true, fmt.Errorf("pre-ledger snapshot equivalence is mutually exclusive with other database actions")
	}
	if err := validateServiceOperationReleaseTransaction(transaction, "update"); err != nil {
		return true, err
	}
	if err := validateServiceOperationReleaseTransactionPath(
		snapshotPath,
		transaction,
		"equivalence snapshot",
	); err != nil {
		return true, err
	}
	return true, nil
}

// proveWALAwarePreLedgerSnapshotLogicalEquivalence materializes the canonical
// database and its last checksum-valid committed WAL prefix into a private
// standalone copy. SQLite never opens the canonical source. Both standalone
// databases must then satisfy the exact pre-ledger schema contract before
// their complete logical contents are compared.
func proveWALAwarePreLedgerSnapshotLogicalEquivalence(
	canonicalPath string,
	standaloneSnapshotPath string,
) error {
	return checkWALAwareServiceOperationsIdleWith(
		canonicalPath,
		func(privateCanonicalPath string) error {
			if err := validateServiceOperationSnapshot(
				privateCanonicalPath,
				serviceOperationSnapshotSchemaPreLedger,
			); err != nil {
				return fmt.Errorf("validate canonical pre-ledger logical snapshot: %w", err)
			}
			if err := validateServiceOperationSnapshot(
				standaloneSnapshotPath,
				serviceOperationSnapshotSchemaPreLedger,
			); err != nil {
				return fmt.Errorf("validate staged pre-ledger snapshot: %w", err)
			}
			if err := compareServiceOperationSnapshotLogicalContents(
				privateCanonicalPath,
				standaloneSnapshotPath,
			); err != nil {
				return fmt.Errorf("compare pre-ledger snapshot logical contents: %w", err)
			}
			return nil
		},
	)
}

// compareServiceOperationSnapshotLogicalContents compares SQLite values, not
// database file bytes. Every ordinary table plus sqlite_sequence is included;
// accessible rowids are included because they can affect application-visible
// behavior. Each cell is represented by its SQLite storage class and a binary
// hex rendering. This preserves NULL/integer/real/text/blob distinctions and
// embedded NUL bytes while avoiding collation-dependent row ordering.
func compareServiceOperationSnapshotLogicalContents(leftPath string, rightPath string) error {
	left, err := openReadOnlyServiceOperationLogicalSnapshot(leftPath)
	if err != nil {
		return fmt.Errorf("open canonical logical snapshot: %w", err)
	}
	defer left.Close()
	right, err := openReadOnlyServiceOperationLogicalSnapshot(rightPath)
	if err != nil {
		return fmt.Errorf("open staged logical snapshot: %w", err)
	}
	defer right.Close()

	ctx, cancel := context.WithTimeout(context.Background(), serviceOperationSnapshotEquivalenceTimeout)
	defer cancel()
	if err := left.PingContext(ctx); err != nil {
		return fmt.Errorf("read canonical logical snapshot: %w", err)
	}
	if err := right.PingContext(ctx); err != nil {
		return fmt.Errorf("read staged logical snapshot: %w", err)
	}

	leftTables, err := readServiceOperationLogicalTables(ctx, left)
	if err != nil {
		return fmt.Errorf("read canonical logical tables: %w", err)
	}
	rightTables, err := readServiceOperationLogicalTables(ctx, right)
	if err != nil {
		return fmt.Errorf("read staged logical tables: %w", err)
	}
	if len(leftTables) != len(rightTables) {
		return fmt.Errorf(
			"logical table count differs: canonical=%d staged=%d",
			len(leftTables),
			len(rightTables),
		)
	}
	for tableIndex := range leftTables {
		leftTable := leftTables[tableIndex]
		rightTable := rightTables[tableIndex]
		if leftTable != rightTable {
			return fmt.Errorf("logical table contract differs at position %d", tableIndex+1)
		}
		if err := compareServiceOperationLogicalTable(ctx, left, right, leftTable); err != nil {
			return err
		}
	}
	return nil
}

func openReadOnlyServiceOperationLogicalSnapshot(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(path, true))
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	return database, nil
}

func readServiceOperationLogicalTables(
	ctx context.Context,
	database *sql.DB,
) ([]serviceOperationLogicalTable, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT schema_table.name, table_list.wr
		FROM sqlite_schema AS schema_table
		JOIN pragma_table_list AS table_list
		  ON table_list.schema = 'main'
		 AND table_list.name = schema_table.name
		WHERE schema_table.type = 'table'
		  AND (schema_table.name NOT LIKE 'sqlite_%' OR schema_table.name = 'sqlite_sequence')
		ORDER BY hex(CAST(schema_table.name AS BLOB)) COLLATE BINARY
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make([]serviceOperationLogicalTable, 0, 32)
	for rows.Next() {
		var table serviceOperationLogicalTable
		var withoutRowID int
		if err := rows.Scan(&table.name, &withoutRowID); err != nil {
			return nil, err
		}
		if table.name == "" || (withoutRowID != 0 && withoutRowID != 1) {
			return nil, fmt.Errorf("logical table metadata is invalid")
		}
		table.withoutRowID = withoutRowID == 1
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tables, nil
}

func readServiceOperationLogicalColumns(
	ctx context.Context,
	database *sql.DB,
	table serviceOperationLogicalTable,
) ([]serviceOperationLogicalColumn, error) {
	rows, err := database.QueryContext(
		ctx,
		"PRAGMA table_xinfo("+quoteServiceOperationSQLiteIdentifier(table.name)+")",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make([]serviceOperationLogicalColumn, 0, 16)
	declaredNames := make(map[string]struct{}, 16)
	for rows.Next() {
		var (
			columnID     int
			name         string
			declaredType string
			notNull      int
			defaultValue any
			primaryKey   int
			hidden       int
		)
		if err := rows.Scan(
			&columnID,
			&name,
			&declaredType,
			&notNull,
			&defaultValue,
			&primaryKey,
			&hidden,
		); err != nil {
			return nil, err
		}
		if columnID < 0 || name == "" || (hidden < 0 || hidden > 3) {
			return nil, fmt.Errorf("logical column metadata is invalid for table %q", table.name)
		}
		declaredNames[strings.ToLower(name)] = struct{}{}
		columns = append(columns, serviceOperationLogicalColumn{
			name:       name,
			expression: quoteServiceOperationSQLiteIdentifier(name),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("logical table %q has no columns", table.name)
	}

	if !table.withoutRowID {
		for _, alias := range []string{"rowid", "_rowid_", "oid"} {
			if _, shadowed := declaredNames[alias]; shadowed {
				continue
			}
			columns = append([]serviceOperationLogicalColumn{{
				name:       "$rowid",
				expression: alias,
			}}, columns...)
			break
		}
	}
	return columns, nil
}

func compareServiceOperationLogicalTable(
	ctx context.Context,
	left *sql.DB,
	right *sql.DB,
	table serviceOperationLogicalTable,
) error {
	leftColumns, err := readServiceOperationLogicalColumns(ctx, left, table)
	if err != nil {
		return fmt.Errorf("read canonical columns for logical table %q: %w", table.name, err)
	}
	rightColumns, err := readServiceOperationLogicalColumns(ctx, right, table)
	if err != nil {
		return fmt.Errorf("read staged columns for logical table %q: %w", table.name, err)
	}
	if len(leftColumns) != len(rightColumns) {
		return fmt.Errorf("logical column count differs for table %q", table.name)
	}
	for columnIndex := range leftColumns {
		if leftColumns[columnIndex].name != rightColumns[columnIndex].name {
			return fmt.Errorf("logical column contract differs for table %q", table.name)
		}
	}

	query := serviceOperationLogicalRowsQuery(table.name, leftColumns)
	leftRows, err := left.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read canonical rows for logical table %q: %w", table.name, err)
	}
	defer leftRows.Close()
	rightRows, err := right.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read staged rows for logical table %q: %w", table.name, err)
	}
	defer rightRows.Close()

	rowNumber := 0
	for {
		leftHasRow := leftRows.Next()
		rightHasRow := rightRows.Next()
		if leftHasRow != rightHasRow {
			return fmt.Errorf("logical row count differs for table %q", table.name)
		}
		if !leftHasRow {
			break
		}
		rowNumber++
		leftCells, err := scanServiceOperationLogicalRow(leftRows, len(leftColumns))
		if err != nil {
			return fmt.Errorf("read canonical logical row %d for table %q: %w", rowNumber, table.name, err)
		}
		rightCells, err := scanServiceOperationLogicalRow(rightRows, len(rightColumns))
		if err != nil {
			return fmt.Errorf("read staged logical row %d for table %q: %w", rowNumber, table.name, err)
		}
		for columnIndex := range leftCells {
			if leftCells[columnIndex] != rightCells[columnIndex] {
				return fmt.Errorf(
					"logical value differs for table %q row %d column %q",
					table.name,
					rowNumber,
					leftColumns[columnIndex].name,
				)
			}
		}
	}
	if err := leftRows.Err(); err != nil {
		return fmt.Errorf("finish canonical logical rows for table %q: %w", table.name, err)
	}
	if err := rightRows.Err(); err != nil {
		return fmt.Errorf("finish staged logical rows for table %q: %w", table.name, err)
	}
	return nil
}

func serviceOperationLogicalRowsQuery(
	tableName string,
	columns []serviceOperationLogicalColumn,
) string {
	selectExpressions := make([]string, 0, len(columns)*2)
	orderExpressions := make([]string, 0, len(columns)*2)
	for columnIndex, column := range columns {
		typeAlias := fmt.Sprintf("__celik_type_%d", columnIndex)
		valueAlias := fmt.Sprintf("__celik_value_%d", columnIndex)
		selectExpressions = append(
			selectExpressions,
			"typeof("+column.expression+") AS "+quoteServiceOperationSQLiteIdentifier(typeAlias),
			"hex("+column.expression+") AS "+quoteServiceOperationSQLiteIdentifier(valueAlias),
		)
		orderExpressions = append(
			orderExpressions,
			quoteServiceOperationSQLiteIdentifier(typeAlias)+" COLLATE BINARY",
			quoteServiceOperationSQLiteIdentifier(valueAlias)+" COLLATE BINARY",
		)
	}
	return "SELECT " + strings.Join(selectExpressions, ", ") +
		" FROM " + quoteServiceOperationSQLiteIdentifier(tableName) +
		" ORDER BY " + strings.Join(orderExpressions, ", ")
}

func scanServiceOperationLogicalRow(
	rows *sql.Rows,
	columnCount int,
) ([]serviceOperationLogicalCell, error) {
	cells := make([]serviceOperationLogicalCell, columnCount)
	destinations := make([]any, 0, columnCount*2)
	for columnIndex := range cells {
		destinations = append(
			destinations,
			&cells[columnIndex].storageClass,
			&cells[columnIndex].valueHex,
		)
	}
	if err := rows.Scan(destinations...); err != nil {
		return nil, err
	}
	for _, cell := range cells {
		switch cell.storageClass {
		case "null":
			if cell.valueHex.Valid && cell.valueHex.String != "" {
				return nil, fmt.Errorf("NULL logical value has a non-empty representation")
			}
		case "integer", "real", "text", "blob":
			if !cell.valueHex.Valid {
				return nil, fmt.Errorf("non-NULL logical value has no representation")
			}
		default:
			return nil, fmt.Errorf("unknown SQLite storage class %q", cell.storageClass)
		}
	}
	return cells, nil
}

func quoteServiceOperationSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
