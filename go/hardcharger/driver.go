// Licensed under the Apache License, Version 2.0.

// Package hardcharger is the ADBC driver for Hardcharger, GizmoData's DuckDB
// 2.0 server, built on the Chargewire ADBC driver
// (github.com/gizmodata/chargewire-adbc/go/v2/chargewire), which does the work:
// Arrow IPC streaming, parameters, bulk ingest, partitioned results, cancel,
// retries, TLS and token sign-in. This package adds:
//
//   - hardcharger://host[:port] URIs, meaning exactly chargewire:// (HTTPS unless
//     ?tls=false), with Hardcharger's port 31337 as the default;
//
//   - "ADBC Hardcharger Driver - Go" as the driver name in GetInfo (the
//     vendor name, "Hardcharger", comes from the server);
//
//   - Hardcharger's options (adbc.hardcharger.*): session and query tags,
//     hardcharger.* settings, browser sign-in, and client certificate
//     sign-in (auth_type=client_certificate, the Chargewire driver's).
//
//     drv := hardcharger.NewDriver(memory.DefaultAllocator)
//     db, err := drv.NewDatabaseWithContext(ctx, map[string]string{
//     "uri":      "hardcharger://db.example.com",
//     "username": "joe",
//     "password": "…",
//     hardcharger.OptionQueryTag: `{"job": "nightly"}`,
//     })
//     cnxn, err := db.Open(ctx)
package hardcharger

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/adbc-drivers/driverbase-go/driverbase"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

// DriverName is what GetInfo reports as the driver name.
const DriverName = "ADBC Hardcharger Driver - Go"

// optionAccessToken is the Chargewire driver's option for a token issued
// elsewhere: here, the server's own token from a browser sign-in.
const optionAccessToken = "adbc.chargewire.auth.access_token"

// NewDriver returns the Hardcharger ADBC driver.
func NewDriver(alloc memory.Allocator) driverbase.Driver {
	return &driverImpl{inner: chargewire.NewDriverWithName(alloc, DriverName)}
}

type driverImpl struct {
	inner driverbase.Driver
}

// NewDatabaseWithContext takes the Chargewire driver's options and
// Hardcharger's; nothing is sent to the server until the first connection
// opens.
func (d *driverImpl) NewDatabaseWithContext(ctx context.Context, opts map[string]string) (adbc.DatabaseWithContext, error) {
	db := &database{cfg: newConfig(), chargewireOptions: map[string]string{}}
	chargewireOpts, err := db.take(opts)
	if err != nil {
		return nil, err
	}
	inner, err := d.inner.NewDatabaseWithContext(ctx, chargewireOpts)
	if err != nil {
		return nil, err
	}
	db.Database = inner.(driverbase.Database)
	return db, nil
}

// database wraps the Chargewire database.
type database struct {
	driverbase.Database

	mu                sync.Mutex
	cfg               *config
	uri               string            // as given (hardcharger://...)
	chargewireURI     string            // as handed to the Chargewire driver
	chargewireOptions map[string]string // Chargewire options seen, for the sign-in's TLS
	signedIn          bool
	// shared and sharedToken are the shared sign-in this database took its
	// token from, when it did not sign in itself (OptionOAuthShareSignIn).
	shared      *sharedSignIn
	sharedToken string
}

// take records Hardcharger's options and translates the URI; it returns the
// options for the Chargewire driver.
func (db *database) take(opts map[string]string) (map[string]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	chargewireOpts := make(map[string]string, len(opts))
	// A map has no order: apply settings in a stable (sorted) one.
	keys := make([]string, 0, len(opts))
	for key := range opts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := opts[key]
		switch {
		case key == OptionIngestParts:
			chargewireOpts[chargewireIngestParts] = value
		case key == OptionAuthType:
			if err := db.cfg.set(key, value); err != nil {
				return nil, err
			}
			chargewireOpts[chargewireAuthType] = chargewireAuthTypeFor(db.cfg.authType)
		case isHardchargerOption(key):
			if err := db.cfg.set(key, value); err != nil {
				return nil, err
			}
		case key == adbc.OptionKeyURI:
			translated, err := ToChargewireURI(value)
			if err != nil {
				return nil, err
			}
			db.uri, db.chargewireURI = value, translated
			chargewireOpts[key] = translated
		default:
			chargewireOpts[key] = value
			db.chargewireOptions[key] = value
		}
	}
	return chargewireOpts, nil
}

func (db *database) SetOption(ctx context.Context, key, value string) error {
	return db.SetOptions(ctx, map[string]string{key: value})
}

func (db *database) SetOptions(ctx context.Context, opts map[string]string) error {
	chargewireOpts, err := db.take(opts)
	if err != nil {
		return err
	}
	// One at a time through SetOption: driverbase's SetOptions is
	// DatabaseImplBase's, which knows none of the Chargewire driver's options.
	keys := slices.Sorted(maps.Keys(chargewireOpts))
	for _, key := range keys {
		if err := db.Database.SetOption(ctx, key, chargewireOpts[key]); err != nil {
			return err
		}
	}
	return nil
}

func (db *database) GetOption(ctx context.Context, key string) (string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	switch {
	case key == OptionIngestParts:
		return db.Database.GetOption(ctx, chargewireIngestParts)
	case isHardchargerOption(key):
		return db.cfg.get(key)
	case key == adbc.OptionKeyURI && db.uri != "":
		return db.uri, nil
	}
	return db.Database.GetOption(ctx, key)
}

// Open signs in through the browser first when auth_type is external, then
// opens a Chargewire connection and applies the session's tags and settings.
func (db *database) Open(ctx context.Context) (adbc.ConnectionWithContext, error) {
	if err := db.signIn(ctx); err != nil {
		return nil, err
	}
	inner, err := db.Database.Open(ctx)
	if err != nil && db.refusedSharedToken(err) {
		// The server refused a shared token it had not seen from this database
		// (revoked, or the server restarted with other keys): sign in afresh, once.
		if err = db.signIn(ctx); err != nil {
			return nil, err
		}
		inner, err = db.Database.Open(ctx)
	}
	if err != nil {
		return nil, err
	}
	cnxn := &connection{Connection: inner.(driverbase.Connection)}
	db.mu.Lock()
	db.shared, db.sharedToken = nil, "" // the server took it: this database is signed in for good
	cfg := *db.cfg
	cfg.settings = append([]setting(nil), db.cfg.settings...)
	db.mu.Unlock()
	if err := cnxn.applyDefaults(ctx, &cfg); err != nil {
		_ = inner.Close(ctx)
		return nil, err
	}
	return cnxn, nil
}

func (db *database) signIn(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.cfg.shareSignIn && db.cfg.authType != AuthTypeExternal {
		return invalidArgument("%s shares a browser sign-in: it needs %s=%s", OptionOAuthShareSignIn,
			OptionAuthType, AuthTypeExternal)
	}
	if db.cfg.authType != AuthTypeExternal || db.signedIn {
		return nil
	}
	chargewireOptions := maps.Clone(db.chargewireOptions)
	chargewireOptions[adbc.OptionKeyURI] = db.chargewireURI
	run := func(ctx context.Context) (string, time.Duration, error) {
		return browserSignIn(ctx, chargewireOptions, db.cfg.browser, db.cfg.oauthTimeout)
	}
	var token string
	var err error
	db.shared, db.sharedToken = nil, ""
	if db.cfg.shareSignIn {
		shared := sharedSignInFor(signInKeyFor(db.chargewireURI, chargewireOptions, db.cfg.signInKey))
		var fresh bool
		if token, fresh, err = shared.get(ctx, run); err != nil {
			return err
		}
		if !fresh {
			db.shared, db.sharedToken = shared, token
		}
	} else if token, _, err = run(ctx); err != nil {
		return err
	}
	if err := db.Database.SetOption(ctx, optionAccessToken, token); err != nil {
		return err
	}
	db.signedIn = true
	return nil
}

// refusedSharedToken reports that opening failed because the server refused
// a token this database took from a shared sign-in, which it then forgets, so
// that the next signIn runs a new sign-in.
func (db *database) refusedSharedToken(err error) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	var adbcErr adbc.Error
	if db.shared == nil || !errors.As(err, &adbcErr) || adbcErr.Code != adbc.StatusUnauthenticated {
		return false
	}
	// Chargewire §4.2: after token_revoked, credentials_changed or principal_dropped the
	// person is told why rather than sent through another sign-in on their behalf.
	if !renewableRefusal(adbcErr) {
		return false
	}
	db.shared.forget(db.sharedToken)
	db.shared, db.sharedToken, db.signedIn = nil, "", false
	return true
}

// connection wraps the Chargewire connection.
type connection struct {
	driverbase.Connection
}

// applyDefaults runs the database's tags and settings on a new session.
func (c *connection) applyDefaults(ctx context.Context, cfg *config) error {
	if cfg.sessionTag != nil {
		if err := c.exec(ctx, setStatement("session_tag", *cfg.sessionTag, false)); err != nil {
			return err
		}
	}
	if cfg.queryTag != nil {
		if err := c.exec(ctx, setStatement("query_tag", *cfg.queryTag, false)); err != nil {
			return err
		}
	}
	for _, s := range cfg.settings {
		if err := c.exec(ctx, setStatement(s.name, s.value, false)); err != nil {
			return err
		}
	}
	return nil
}

func (c *connection) SetOption(ctx context.Context, key, value string) error {
	if key == OptionIngestParts {
		key = chargewireIngestParts
	}
	name, global, err := connectionSetting(key)
	if err != nil {
		return err
	}
	if name == "" {
		return c.Connection.SetOption(ctx, key, value)
	}
	return c.exec(ctx, setStatement(name, value, global))
}

func (c *connection) GetOption(ctx context.Context, key string) (string, error) {
	if key == OptionIngestParts {
		key = chargewireIngestParts
	}
	name, _, err := connectionSetting(key)
	if err != nil {
		return "", err
	}
	if name == "" {
		return c.Connection.GetOption(ctx, key)
	}
	return c.setting(ctx, name)
}

// connectionSetting maps a connection option to the hardcharger.NAME setting
// it reads or writes ("" for options that aren't Hardcharger's).
func connectionSetting(key string) (name string, global bool, err error) {
	switch {
	case key == OptionSessionTag:
		return "session_tag", false, nil
	case key == OptionQueryTag:
		return "query_tag", false, nil
	case strings.HasPrefix(key, OptionSettingPrefix):
		name, err = settingName(strings.TrimPrefix(key, OptionSettingPrefix))
		return name, false, err
	case strings.HasPrefix(key, OptionGlobalSettingPrefix):
		name, err = settingName(strings.TrimPrefix(key, OptionGlobalSettingPrefix))
		return name, true, err
	case isHardchargerOption(key):
		return "", false, invalidArgument("%s is a database option: set it before connecting", key)
	}
	return "", false, nil
}

// exec runs a statement without a result set on this connection's session.
func (c *connection) exec(ctx context.Context, sql string) error {
	stmt, err := c.NewStatement(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close(ctx) }()
	if err := stmt.SetSqlQuery(ctx, sql); err != nil {
		return err
	}
	_, err = stmt.ExecuteUpdate(ctx)
	return err
}

// setting reads hardcharger.NAME as it applies to this session.
func (c *connection) setting(ctx context.Context, name string) (string, error) {
	stmt, err := c.NewStatement(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stmt.Close(ctx) }()
	if err := stmt.SetSqlQuery(ctx, "SELECT CAST(value AS VARCHAR) FROM hardcharger_settings() WHERE name = ?"); err != nil {
		return "", err
	}
	if err := stmt.Prepare(ctx); err != nil {
		return "", err
	}
	builder := array.NewRecordBuilder(memory.DefaultAllocator,
		arrow.NewSchema([]arrow.Field{{Name: "name", Type: arrow.BinaryTypes.String}}, nil))
	defer builder.Release()
	builder.Field(0).(*array.StringBuilder).Append("hardcharger." + name)
	params := builder.NewRecordBatch()
	defer params.Release()
	if err := stmt.Bind(ctx, params); err != nil {
		return "", err
	}
	reader, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Release()
	for reader.Next() {
		rec := reader.RecordBatch()
		if rec.NumRows() > 0 {
			column := rec.Column(0).(*array.String)
			if column.IsNull(0) {
				return "", nil
			}
			// Copy: Value aliases the batch's buffer, which Release frees.
			return strings.Clone(column.Value(0)), nil
		}
	}
	if err := reader.Err(); err != nil {
		return "", err
	}
	return "", adbc.Error{Code: adbc.StatusNotFound,
		Msg: "[hardcharger] no setting hardcharger." + name + " visible to this session"}
}

var _ driverbase.Driver = (*driverImpl)(nil)
var _ driverbase.Database = (*database)(nil)
var _ driverbase.Connection = (*connection)(nil)

// renewableRefusal reports whether a refused token may be replaced by signing in
// again (Chargewire §4.2): a refusal without a reason, token_expired, or a reason this
// driver doesn't know.
func renewableRefusal(err adbc.Error) bool {
	for _, detail := range err.Details {
		if detail.Key() != chargewire.ErrorDetailReason {
			continue
		}
		value, serr := detail.Serialize()
		if serr != nil {
			return true
		}
		switch string(value) {
		case "token_revoked", "credentials_changed", "principal_dropped":
			return false
		}
	}
	return true
}
