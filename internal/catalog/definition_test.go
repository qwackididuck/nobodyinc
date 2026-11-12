package catalog

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		want    Definition
		wantErr string
	}{
		{
			name: "full agent",
			file: `---
role: Go developer
model: sonnet
permissions:
  profile: allowlist
  allow: ["Bash(go test *)"]
  deny: ["Read(./.env)"]
tools: [Read, Edit]
mcp: [gitlab]
skills: [drawio]
from: dev@2
on_leave: true
---

You write Go.
With tests.
`,
			want: Definition{
				Role:  "Go developer",
				Model: "sonnet",
				Permissions: Permissions{
					Profile: ProfileAllowlist,
					Allow:   []string{"Bash(go test *)"},
					Deny:    []string{"Read(./.env)"},
				},
				Tools:   []string{"Read", "Edit"},
				MCP:     []string{"gitlab"},
				Skills:  []string{"drawio"},
				From:    "dev@2",
				OnLeave: true,
				Prompt:  "You write Go.\nWith tests.",
			},
		},
		{
			name: "template fields",
			file: "---\ndescription: Writes Go\nversion: 3\n---\nBody\n",
			want: Definition{Description: "Writes Go", Version: 3, Prompt: "Body"},
		},
		{
			name: "empty front matter",
			file: "---\n---\nJust a prompt.\n",
			want: Definition{Prompt: "Just a prompt."},
		},
		{
			name: "no body, closing fence at end of file",
			file: "---\nrole: Tester\n---",
			want: Definition{Role: "Tester"},
		},
		{
			name: "windows line endings",
			file: "---\r\nrole: Tester\r\n---\r\nBody\r\n",
			want: Definition{Role: "Tester", Prompt: "Body"},
		},
		{
			name:    "no front matter",
			file:    "You write Go.\n",
			wantErr: "must start with",
		},
		{
			name:    "unclosed front matter",
			file:    "---\nrole: Tester\nBody\n",
			wantErr: "not closed",
		},
		{
			name:    "unknown field (typo)",
			file:    "---\nrole: Tester\ntemperature: 3\n---\n",
			wantErr: "temperature",
		},
		{
			name:    "auto profile with haiku",
			file:    "---\nmodel: haiku\npermissions: {profile: auto}\n---\n",
			wantErr: "Haiku",
		},
		{
			name:    "unknown profile",
			file:    "---\npermissions: {profile: yolo}\n---\n",
			wantErr: "permissions.profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse([]byte(tt.file))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestFormatRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		def  Definition
	}{
		{name: "empty", def: Definition{}},
		{name: "prompt only", def: Definition{Prompt: "Hello."}},
		{
			name: "everything",
			def: Definition{
				Description: "d", Version: 2, Role: "r", Model: "opus",
				Permissions: Permissions{Profile: ProfileAuto, Allow: []string{"Edit"}, Deny: []string{"Read(./.env)"}},
				Tools:       []string{"Read"}, MCP: []string{"gitlab"}, Skills: []string{"drawio"},
				From: "dev@1", OnLeave: true, Prompt: "Line 1\n\nLine 2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := Format(tt.def)
			if err != nil {
				t.Fatal(err)
			}

			got, err := Parse(data)
			if err != nil {
				t.Fatalf("parse formatted file:\n%s\nerror: %v", data, err)
			}

			if !reflect.DeepEqual(got, tt.def) {
				t.Fatalf("round trip changed the definition:\n%s\ngot  %+v\nwant %+v", data, got, tt.def)
			}
		})
	}
}

func TestEffectiveProfile(t *testing.T) {
	t.Parallel()

	if got := (Permissions{}).EffectiveProfile(); got != ProfileDefault {
		t.Fatalf("empty profile = %q, want %q", got, ProfileDefault)
	}

	if got := (Permissions{Profile: ProfileAuto}).EffectiveProfile(); got != ProfileAuto {
		t.Fatalf("auto profile = %q, want %q", got, ProfileAuto)
	}
}
