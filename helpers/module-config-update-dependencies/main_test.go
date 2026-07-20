package main

import (
	"os"
	"reflect"
	"testing"
)

func TestCleanModulePath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "root", in: ".", want: "."},
		{name: "nested", in: "polyfill/.", want: "polyfill"},
		{name: "absolute", in: "/polyfill", wantErr: true},
		{name: "escape", in: "../polyfill", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := cleanModulePath(test.in)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestDaggerJSONPath(t *testing.T) {
	tests := map[string]string{
		".":        "dagger.json",
		"polyfill": "polyfill/dagger.json",
	}

	for in, want := range tests {
		if got := daggerJSONPath(in); got != want {
			t.Fatalf("daggerJSONPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMockSourcePath(t *testing.T) {
	tests := map[string]string{
		".":        "/mock",
		"polyfill": "/mock/polyfill",
	}

	for in, want := range tests {
		if got := mockSourcePath(in); got != want {
			t.Fatalf("mockSourcePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpdatesFromEnv(t *testing.T) {
	t.Setenv(updatesJSONEnv, `["one","two"]`)

	got, err := updatesFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"one", "two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEnvString(t *testing.T) {
	t.Setenv("TEST_STRING", `"decoded"`)
	got, err := envString("TEST_STRING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "decoded" {
		t.Fatalf("got %q, want decoded", got)
	}

	if err := os.Setenv("TEST_STRING", `{"json":"object"}`); err != nil {
		t.Fatal(err)
	}
	got, err = envString("TEST_STRING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"json":"object"}` {
		t.Fatalf("got %q, want raw JSON object", got)
	}
}
