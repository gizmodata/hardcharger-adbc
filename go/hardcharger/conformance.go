// Licensed under the Apache License, Version 2.0.

package hardcharger

// The ADBC statuses the specification asks for in three cases where the
// Chargewire driver reports another, found by the ADBC Driver Foundry's
// validation suite (validation/):
//
//   - setting an option the driver doesn't know on a database that has opened
//     a connection is StatusNotImplemented, as before the first connection
//     (the Chargewire driver says StatusInvalidState: too late to set it);
//   - making a catalog or schema that doesn't exist current is
//     StatusNotFound (the server refuses the session option, which the
//     Chargewire driver reports as StatusInvalidArgument);
//   - appending (or create_append) data with a column the existing table
//     doesn't have is StatusAlreadyExists, the specification's status for "the
//     table exists with another schema" (the server's binder error is a
//     sql_error, StatusInvalidArgument).
//
// Each is decided by asking (an unopened Chargewire database, GetObjects,
// GetTableSchema), never by reading the server's message, and only on the
// error path. They belong in chargewire-adbc, where every Chargewire driver
// gets them; until it has them, this driver does.

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// unknownOption reports whether key is an option the Chargewire driver doesn't
// know: a database that never connected refuses it as not implemented.
func (db *database) unknownOption(ctx context.Context, key, value string) error {
	probe, err := db.driver.NewDatabaseWithContext(ctx, nil)
	if err != nil {
		return nil
	}
	defer func() { _ = probe.Close(ctx) }()
	err = probe.(adbc.GetSetOptionsWithContext).SetOption(ctx, key, value)
	if status(err) == adbc.StatusNotImplemented {
		return err
	}
	return nil
}

// status is err's ADBC status, or StatusOK for an error that isn't ADBC's.
func status(err error) adbc.Status {
	var adbcErr adbc.Error
	if errors.As(err, &adbcErr) {
		return adbcErr.Code
	}
	return adbc.StatusOK
}

// withStatus returns err, an ADBC error, with another status; its message and
// details stay.
func withStatus(err error, code adbc.Status) error {
	var adbcErr adbc.Error
	if !errors.As(err, &adbcErr) {
		return err
	}
	adbcErr.Code = code
	return adbcErr
}

// setCurrentNamespace sets the current catalog or schema; a name that doesn't
// exist is StatusNotFound.
func (c *connection) setCurrentNamespace(ctx context.Context, key, value string) error {
	err := c.Connection.SetOption(ctx, key, value)
	if err == nil || status(err) != adbc.StatusInvalidArgument {
		return err
	}
	var catalog, schema *string
	depth := adbc.ObjectDepthCatalogs
	if key == adbc.OptionKeyCurrentCatalog {
		catalog = &value
	} else {
		current, cerr := c.Connection.GetOption(ctx, adbc.OptionKeyCurrentCatalog)
		if cerr != nil {
			return err
		}
		catalog, schema, depth = &current, &value, adbc.ObjectDepthDBSchemas
	}
	exists, cerr := c.namespaceExists(ctx, depth, catalog, schema)
	if cerr != nil || exists {
		return err
	}
	return withStatus(err, adbc.StatusNotFound)
}

// namespaceExists reports whether GetObjects lists the catalog (and, at
// ObjectDepthDBSchemas, the schema in it) by exactly that name: GetObjects'
// filters are patterns.
func (c *connection) namespaceExists(ctx context.Context, depth adbc.ObjectDepth, catalog, schema *string) (bool, error) {
	reader, err := c.GetObjects(ctx, depth, catalog, schema, nil, nil, nil)
	if err != nil {
		return false, err
	}
	defer reader.Release()
	for reader.Next() {
		rec := reader.RecordBatch()
		names, ok := rec.Column(0).(*array.String)
		if !ok {
			return false, errors.New("GetObjects: catalog_name is not a string column")
		}
		schemas, _ := rec.Column(1).(*array.List)
		for i := range int(rec.NumRows()) {
			if names.IsNull(i) || names.Value(i) != *catalog {
				continue
			}
			if schema == nil {
				return true, nil
			}
			if schemas == nil || schemas.IsNull(i) {
				continue
			}
			structs, ok := schemas.ListValues().(*array.Struct)
			if !ok {
				return false, errors.New("GetObjects: catalog_db_schemas is not a list of structs")
			}
			schemaNames, ok := structs.Field(0).(*array.String)
			if !ok {
				return false, errors.New("GetObjects: db_schema_name is not a string column")
			}
			start, end := schemas.ValueOffsets(i)
			for j := int(start); j < int(end); j++ {
				if !schemaNames.IsNull(j) && schemaNames.Value(j) == *schema {
					return true, nil
				}
			}
		}
	}
	return false, reader.Err()
}

func (c *connection) NewStatement(ctx context.Context) (adbc.StatementWithContext, error) {
	inner, err := c.Connection.NewStatement(ctx)
	if err != nil {
		return nil, err
	}
	st, ok := inner.(driverbase.Statement)
	if !ok {
		return inner, nil
	}
	return &statement{Statement: st, cnxn: c}, nil
}

// statement wraps the Chargewire statement; it follows the bulk ingest's
// options and data, to tell a failed append into a table with other columns.
type statement struct {
	driverbase.Statement
	cnxn *connection

	ingestTable, ingestCatalog, ingestSchema, ingestMode string
	ingestTemporary                                      bool
	// bound is the schema of the data bound last (Bind or BindStream).
	bound *arrow.Schema
}

var _ driverbase.Statement = (*statement)(nil)
var _ driverbase.StatementCanceler = (*statement)(nil)
var _ adbc.StatementExecuteSchema = (*statement)(nil)

func (st *statement) SetOption(ctx context.Context, key, value string) error {
	if err := st.Statement.SetOption(ctx, key, value); err != nil {
		return err
	}
	switch key {
	case adbc.OptionKeyIngestTargetTable:
		st.ingestTable = value
	case adbc.OptionKeyIngestMode:
		st.ingestMode = value
	case adbc.OptionValueIngestTargetCatalog:
		st.ingestCatalog = value
	case adbc.OptionValueIngestTargetDBSchema:
		st.ingestSchema = value
	case adbc.OptionValueIngestTemporary:
		st.ingestTemporary, _ = strconv.ParseBool(value)
	}
	return nil
}

// SetSqlQuery: the Chargewire statement runs the query, not an ingest, after it.
func (st *statement) SetSqlQuery(ctx context.Context, query string) error {
	if err := st.Statement.SetSqlQuery(ctx, query); err != nil {
		return err
	}
	st.ingestTable = ""
	return nil
}

func (st *statement) Bind(ctx context.Context, values arrow.RecordBatch) error {
	if err := st.Statement.Bind(ctx, values); err != nil {
		return err
	}
	st.bound = nil
	if values != nil {
		st.bound = values.Schema()
	}
	return nil
}

func (st *statement) BindStream(ctx context.Context, stream array.RecordReader) error {
	if err := st.Statement.BindStream(ctx, stream); err != nil {
		return err
	}
	st.bound = nil
	if stream != nil {
		st.bound = stream.Schema()
	}
	return nil
}

func (st *statement) ExecuteQuery(ctx context.Context) (array.RecordReader, int64, error) {
	reader, affected, err := st.Statement.ExecuteQuery(ctx)
	if err != nil {
		err = st.ingestError(ctx, err)
	}
	return reader, affected, err
}

func (st *statement) ExecuteUpdate(ctx context.Context) (int64, error) {
	affected, err := st.Statement.ExecuteUpdate(ctx)
	if err != nil {
		err = st.ingestError(ctx, err)
	}
	return affected, err
}

func (st *statement) Cancel(ctx context.Context) error {
	if canceler, ok := st.Statement.(driverbase.StatementCanceler); ok {
		return canceler.Cancel(ctx)
	}
	return adbc.Error{Code: adbc.StatusNotImplemented, Msg: "[hardcharger] Cancel is not supported"}
}

func (st *statement) ExecuteSchema(ctx context.Context) (*arrow.Schema, error) {
	if es, ok := st.Statement.(adbc.StatementExecuteSchema); ok {
		return es.ExecuteSchema(ctx)
	}
	return nil, adbc.Error{Code: adbc.StatusNotImplemented, Msg: "[hardcharger] ExecuteSchema is not supported"}
}

// ingestError is StatusAlreadyExists for an append (or create_append) that
// failed as an invalid argument because the data has a column the table
// doesn't. DuckDB binds the data's columns to the table's by name, case
// insensitively; a table may have columns the data doesn't (they take their
// defaults).
func (st *statement) ingestError(ctx context.Context, err error) error {
	if st.ingestTable == "" || st.bound == nil || status(err) != adbc.StatusInvalidArgument {
		return err
	}
	if st.ingestMode != adbc.OptionValueIngestModeAppend && st.ingestMode != adbc.OptionValueIngestModeCreateAppend {
		return err
	}
	var catalog, schema *string
	if st.ingestCatalog != "" {
		catalog = &st.ingestCatalog
	}
	if st.ingestSchema != "" {
		schema = &st.ingestSchema
	}
	if st.ingestTemporary {
		temp, main := "temp", "main"
		catalog, schema = &temp, &main
	}
	table, terr := st.cnxn.GetTableSchema(ctx, catalog, schema, st.ingestTable)
	if terr != nil {
		return err
	}
	for _, field := range st.bound.Fields() {
		found := false
		for _, column := range table.Fields() {
			if strings.EqualFold(column.Name, field.Name) {
				found = true
				break
			}
		}
		if !found {
			return withStatus(err, adbc.StatusAlreadyExists)
		}
	}
	return err
}
