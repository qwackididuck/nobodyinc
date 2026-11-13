package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    *string // nil: no file
		want    Settings
		wantErr string
	}{
		{name: "no file: defaults", want: Defaults()},
		{name: "comments only: defaults", file: new("# nothing yet\n"), want: Defaults()},
		{name: "warn", file: new("config_protection: warn\n"), want: Settings{ConfigProtection: ProtectWarn}},
		{name: "invalid value", file: new("config_protection: maybe\n"), wantErr: "config_protection"},
		{name: "unknown key", file: new("config_protecton: warn\n"), wantErr: "config_protecton"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			if tt.file != nil {
				if err := os.WriteFile(filepath.Join(home, FileName), []byte(*tt.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got, err := Load(home)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Fatalf("Load = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestWriteDefaultIfMissing(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	wrote, err := WriteDefaultIfMissing(home)
	if err != nil || !wrote {
		t.Fatalf("first write: wrote=%v err=%v", wrote, err)
	}

	if got, err := Load(home); err != nil || got != Defaults() {
		t.Fatalf("the default file should load as the defaults: %+v, %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(home, FileName), []byte("config_protection: warn\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if wrote, err := WriteDefaultIfMissing(home); err != nil || wrote {
		t.Fatalf("an existing file must be kept: wrote=%v err=%v", wrote, err)
	}
}
