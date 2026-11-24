package daemon

import (
	"context"

	"github.com/qwackididuck/nobodyinc/internal/projectenv"
	"github.com/qwackididuck/nobodyinc/internal/store"
)

// openStores opens the project's database and reads its environment
// variables. The variables come first, so that a broken file leaves no
// database open.
func openStores(ctx context.Context, opts Options) (*store.Store, *projectenv.Store, error) {
	env, err := openProjectEnv(opts.EnvFile)
	if err != nil {
		return nil, nil, err
	}

	st, err := store.Open(ctx, opts.DBPath)
	if err != nil {
		return nil, nil, err
	}

	return st, env, nil
}

// openProjectEnv reads the project's environment variables (ADR-0020,
// docs/projectenv.md). An empty path gives a nil store, which holds nothing
// and overlays nothing.
func openProjectEnv(path string) (*projectenv.Store, error) {
	if path == "" {
		return nil, nil //nolint:nilnil // a nil *projectenv.Store is the documented "no variables"
	}

	return projectenv.Open(path)
}
