package org

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExample(t *testing.T) {
	t.Parallel()

	o, err := Load(filepath.Join("..", "..", "nobody.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := o.Member("archi@specs"); !ok {
		t.Fatal("archi@specs not found in the example org")
	}

	if !o.HasChannel("dev", "specs") || o.HasChannel("specs", "dev") {
		t.Fatal("example should declare dev -> specs only")
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid",
			yaml: `
teams:
  - id: specs
    agents: [{name: archi}]
  - id: dev
    agents: [{name: dev1}]
channels:
  - {from: dev, to: specs}`,
		},
		{
			name:    "duplicate team",
			yaml:    "teams: [{id: dev}, {id: dev}]",
			wantErr: true,
		},
		{
			name:    "duplicate agent in a team",
			yaml:    "teams: [{id: dev, agents: [{name: a}, {name: a}]}]",
			wantErr: true,
		},
		{
			name:    "invalid name",
			yaml:    "teams: [{id: Dev Team}]",
			wantErr: true,
		},
		{
			name:    "channel to unknown team",
			yaml:    "teams: [{id: dev}]\nchannels: [{from: dev, to: ghosts}]",
			wantErr: true,
		},
		{
			name:    "channel to itself",
			yaml:    "teams: [{id: dev}]\nchannels: [{from: dev, to: dev}]",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "org.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTeamOf(t *testing.T) {
	t.Parallel()

	if got := TeamOf("archi@specs"); got != "specs" {
		t.Fatalf("TeamOf = %q", got)
	}

	if got := TeamOf(HumanAddress); got != "" {
		t.Fatalf("TeamOf(human) = %q, want empty", got)
	}
}
