package frontmatter

import (
	"testing"
)

func TestParseAndFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		dropped   []string
		wantDesc  string
		wantBody  string
		wantOther string
		wantLoose bool
	}{
		{
			name:     "no header: all body",
			input:    "Just instructions.\n",
			wantBody: "Just instructions.",
		},
		{
			name:      "valid header, dropped key discarded",
			input:     "---\nname: x\ndescription: Does X.\n# a comment\nargument-hint: [file]\n---\nBody.\n",
			dropped:   []string{"name"},
			wantDesc:  "Does X.",
			wantBody:  "Body.",
			wantOther: "# a comment\nargument-hint: [file]\n",
		},
		{
			name:      "header that is not valid YAML: read line by line",
			input:     "---\ndescription: Reviews: tests, docs.\nallowed-tools: Bash(git log *)\n---\nCheck.\n",
			wantDesc:  "Reviews: tests, docs.",
			wantBody:  "Check.",
			wantOther: "allowed-tools: Bash(git log *)\n",
			wantLoose: true,
		},
		{
			name:     "empty header",
			input:    "---\n---\nBody.\n",
			wantBody: "Body.",
		},
		{
			name:     "header never closed: all body",
			input:    "---\ndescription: x\n",
			wantBody: "---\ndescription: x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Parse([]byte(tt.input), tt.dropped...)
			if err != nil {
				t.Fatal(err)
			}

			other, err := f.OtherKeys()
			if err != nil {
				t.Fatal(err)
			}

			if f.Description != tt.wantDesc || f.Body != tt.wantBody || other != tt.wantOther || (f.looseOther != "") != tt.wantLoose {
				t.Fatalf("Parse = %+v, other keys %q", f, other)
			}

			// Written back and read again, nothing is lost, and the header is
			// valid YAML.
			data, err := f.Format()
			if err != nil {
				t.Fatal(err)
			}

			again, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}

			againOther, _ := again.OtherKeys()
			if again.Description != f.Description || again.Body != f.Body || againOther != other || again.looseOther != "" {
				t.Errorf("round trip of\n%s= %+v, other keys %q", data, again, againOther)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    File
		leading []Field
		want    string
	}{
		{name: "body only", file: File{Body: "Do it."}, want: "Do it.\n"},
		{name: "description and body", file: File{Description: "Does it.", Body: "Do it."}, want: "---\ndescription: Does it.\n---\n\nDo it.\n"},
		{
			name: "leading field first, colon quoted", file: File{Description: "A: b", Body: "x"},
			leading: []Field{{Key: "name", Value: "n"}}, want: "---\nname: n\ndescription: 'A: b'\n---\n\nx\n",
		},
		{name: "nothing", file: File{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.file.Format(tt.leading...)
			if err != nil || string(got) != tt.want {
				t.Errorf("Format = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestHas(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"---\ndescription: ok\nallowed-tools: Read\n---\n",
		"---\ndescription: not: valid\nallowed-tools: Read\n---\n",
	} {
		f, err := Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}

		if !f.Has("allowed-tools") || f.Has("hooks") || f.Has("allowed") {
			t.Errorf("Has on %q: allowed-tools %v, hooks %v, allowed %v", input, f.Has("allowed-tools"), f.Has("hooks"), f.Has("allowed"))
		}
	}

	for _, header := range []string{"just some words", "description: a: b\njust some words"} {
		if _, err := Parse([]byte("---\n" + header + "\n---\n")); err == nil {
			t.Errorf("header %q is not keys and values: it should be refused", header)
		}
	}
}

func TestValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, header, key, want string
	}{
		{name: "scalar", header: "argument-hint: [file] [line]", key: "argument-hint", want: "[file] [line]"},
		{name: "list in brackets keeps them", header: "argument-hint: [pr]", key: "argument-hint", want: "[pr]"},
		{name: "list", header: "allowed-tools:\n  - Read\n  - Bash(git log *)", key: "allowed-tools", want: "Read, Bash(git log *)"},
		{name: "absent", header: "model: haiku", key: "allowed-tools", want: ""},
		{name: "loose header", header: "description: a: b\nallowed-tools: Bash(go test *)", key: "allowed-tools", want: "Bash(go test *)"},
		{name: "loose header, key prefix of another", header: "description: a: b\nallowed-tools-x: y", key: "allowed-tools", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Parse([]byte("---\n" + tt.header + "\n---\nBody.\n"))
			if err != nil {
				t.Fatal(err)
			}

			if got := f.Value(tt.key); got != tt.want {
				t.Errorf("Value(%s) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
