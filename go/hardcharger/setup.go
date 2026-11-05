// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

// sessionSetup is what a connection sets up with SQL in each of its sessions:
// the database's tags and adbc.hardcharger.setting.* settings, as the
// connection's own SetOption calls changed them since. The Chargewire driver
// runs it (a new-session hook) on the connection's first session and on every
// session that replaces a lost one, before anything else runs there, so a
// replacement has the same settings and tags as the session it replaces.
// Global settings live on the server, not in a session, and are not part of it.
type sessionSetup struct {
	mu         sync.Mutex
	sessionTag *string
	queryTag   *string
	settings   []setting // one per name, in the order first given
	// applied: the hook ran on a session (it doesn't on one the connection
	// attached to, adbc.chargewire.session.id).
	applied atomic.Bool
}

func newSessionSetup(cfg *config) *sessionSetup {
	s := &sessionSetup{}
	if cfg.sessionTag != nil {
		s.record("session_tag", *cfg.sessionTag)
	}
	if cfg.queryTag != nil {
		s.record("query_tag", *cfg.queryTag)
	}
	for _, setting := range cfg.settings {
		s.record(setting.name, setting.value)
	}
	return s
}

// record notes that hardcharger.name was set to value in the session (SET
// SESSION, or RESET for ""), so that later sessions get it too.
func (s *sessionSetup) record(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch name {
	case "session_tag":
		s.sessionTag = &value
		return
	case "query_tag":
		s.queryTag = &value
		return
	}
	for i := range s.settings {
		if s.settings[i].name == name {
			s.settings[i].value = value
			return
		}
	}
	s.settings = append(s.settings, setting{name: name, value: value})
}

// statements are the setup's SET (and RESET) statements: the tags first, then
// the settings.
func (s *sessionSetup) statements() []setting {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []setting
	if s.sessionTag != nil {
		all = append(all, setting{name: "session_tag", value: *s.sessionTag})
	}
	if s.queryTag != nil {
		all = append(all, setting{name: "query_tag", value: *s.queryTag})
	}
	return append(all, s.settings...)
}

// apply is the new-session hook: it runs the setup in session. The first
// failure stops it; the Chargewire driver then ends the session rather than
// use it without its settings (Open fails, or the request that found the old
// session lost fails and the next one tries again with another session).
func (s *sessionSetup) apply(ctx context.Context, session chargewire.NewSession) error {
	s.applied.Store(true)
	if name, err := s.run(ctx, session.Exec); err != nil {
		return fmt.Errorf("setting hardcharger.%s: %w", name, err)
	}
	return nil
}

// run runs the setup's statements with exec, and returns the setting whose
// statement failed with exec's error, unchanged.
func (s *sessionSetup) run(ctx context.Context, exec func(ctx context.Context, sql string) error) (string, error) {
	for _, setting := range s.statements() {
		if err := exec(ctx, setStatement(setting.name, setting.value, false)); err != nil {
			return setting.name, err
		}
	}
	return "", nil
}
