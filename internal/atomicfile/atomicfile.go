// Package atomicfile writes files so that readers never see a half-written
// one: the content goes to a temporary file in the same directory, which is
// then renamed over the target (a rename is atomic on the same filesystem).
//
// Configuration files (templates, agent definitions) are edited by the UI
// while the daemon and the user's editor may read them, hence this package.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// filePerm is the mode of written files: they hold no secret (ADR-0004) and
// are meant to be committed or shared.
const filePerm = 0o644

// dirPerm is the mode of directories created on the way.
const dirPerm = 0o755

// Write replaces path with data, creating missing parent directories.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	// Removing the temporary file fails harmlessly once it has been renamed.
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := writeAndClose(tmp, data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // the write error is the one worth reporting

		return err
	}

	if err := f.Chmod(filePerm); err != nil {
		_ = f.Close() // the chmod error is the one worth reporting

		return err
	}

	return f.Close()
}
