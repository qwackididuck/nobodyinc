package webui

import (
	"testing"
	"testing/fstest"
)

func TestBuilt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files fstest.MapFS
		want  bool
	}{
		{name: "an export", files: fstest.MapFS{"index.html": {}, "_next/app.js": {}}, want: true},
		{name: "only the README of dist/", files: fstest.MapFS{"README.md": {}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Built(tt.files); got != tt.want {
				t.Errorf("Built = %v, want %v", got, tt.want)
			}
		})
	}
}
