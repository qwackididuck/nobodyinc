package main

import "testing"

func TestPathFromOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		out     string
		want    string
		wantErr bool
	}{
		{name: "alone", out: pathMarker + "/a:/b" + pathMarker, want: "/a:/b"},
		{name: "after what the startup files print", out: "Welcome!\n" + pathMarker + "/a:/b" + pathMarker + "\n", want: "/a:/b"},
		{name: "no marker", out: "/a:/b", wantErr: true},
		{name: "empty PATH", out: pathMarker + pathMarker, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := pathFromOutput(tt.out)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("pathFromOutput = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// The real login shell answers with a PATH.
func TestLoginShellPath(t *testing.T) {
	t.Parallel()

	path, err := loginShellPath("/bin/sh")
	if err != nil || path == "" {
		t.Fatalf("loginShellPath = %q, %v", path, err)
	}
}
