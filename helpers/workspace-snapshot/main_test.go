package main

import (
	"reflect"
	"testing"
)

func TestIncludeDirectory(t *testing.T) {
	got := includeDirectory("app")
	want := []string{"app", "app/**", "app/**/.*/**"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("include mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCleanPathsRejectEscapes(t *testing.T) {
	if _, err := cleanWorkspacePath("../out"); err == nil {
		t.Fatal("cleanWorkspacePath accepted escaping path")
	}
	if _, err := cleanLocalPath("../out"); err == nil {
		t.Fatal("cleanLocalPath accepted escaping path")
	}
}
