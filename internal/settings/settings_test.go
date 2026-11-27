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
		{name: "warn", file: new("config_protection: warn\n"), want: Settings{ConfigProtection: ProtectWarn, CheckUpdates: true, CloseOpenAfterDays: DefaultCloseOpenAfterDays}},
		{name: "updates check off", file: new("check_updates: false\n"), want: Settings{ConfigProtection: ProtectBlock, CloseOpenAfterDays: DefaultCloseOpenAfterDays}},
		{name: "updates check on", file: new("check_updates: true\n"), want: Defaults()},
		{name: "updates check not a boolean", file: new("check_updates: sometimes\n"), wantErr: "cannot unmarshal"},
		{name: "close after 3 days", file: new("close_open_after_days: 3\n"),
			want: Settings{ConfigProtection: ProtectBlock, CheckUpdates: true, CloseOpenAfterDays: 3}},
		{name: "never close", file: new("close_open_after_days: 0\n"), want: Settings{ConfigProtection: ProtectBlock, CheckUpdates: true}},
		{name: "negative days", file: new("close_open_after_days: -1\n"), wantErr: "close_open_after_days"},
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

func TestLiveSetConfigProtection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    string // in force and in the file afterwards
	}{
		{"warn is saved", ProtectWarn, false, ProtectWarn},
		{"block is saved", ProtectBlock, false, ProtectBlock},
		{"anything else is refused and changes nothing", "maybe", true, ProtectBlock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			live := NewLive(home, Defaults())

			_, err := live.SetConfigProtection(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}

			if got := live.Current().ConfigProtection; got != tt.want {
				t.Fatalf("in force: %q, want %q", got, tt.want)
			}

			if tt.wantErr {
				return
			}

			saved, err := Load(home)
			if err != nil || saved.ConfigProtection != tt.want {
				t.Fatalf("saved file: %+v, %v; want %q", saved, err, tt.want)
			}
		})
	}
}
