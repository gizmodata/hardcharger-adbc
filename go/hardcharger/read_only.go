// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"

	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

// WithReadOnly requests a server-confirmed read-only session before session setup.
// The Chargewire driver owns confirmation, one-way transitions and replacement.
func WithReadOnly(ctx context.Context) context.Context {
	return chargewire.WithReadOnly(ctx)
}

// ConnectionInitContext carries pre-initialization C connection options into Open.
func ConnectionInitContext(ctx context.Context, options map[string]string) (context.Context, error) {
	return chargewire.ConnectionInitContext(ctx, options)
}

// SetInitOption preserves the upstream one-way rule before C connection initialization.
func SetInitOption(options map[string]string, key, value string) error {
	return chargewire.SetInitOption(options, key, value)
}
