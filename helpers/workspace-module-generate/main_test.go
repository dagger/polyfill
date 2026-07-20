package main

import (
	"context"
	"reflect"
	"testing"
)

func TestModuleSourceIncludeFromConfigsIncludesDeclaredPaths(t *testing.T) {
	configs := map[string]sourceConfig{
		"app/dagger.json": {
			dependencies: []string{"../dep"},
			include:      []string{"../root.txt", "assets/**/*"},
		},
		"dep/dagger.json": {
			include: []string{"../shared.txt", "subdir/**/*"},
		},
	}

	got, err := moduleSourceIncludeFromConfigs(configs, "app")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"app",
		"app/**",
		"app/assets/**/*",
		"app/dagger.json",
		"dep",
		"dep/**",
		"dep/dagger.json",
		"dep/subdir/**/*",
		"root.txt",
		"shared.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("include mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestParseSourceConfigTOMLReadsDependenciesAndInclude(t *testing.T) {
	contents := `name = "app"
engineVersion = "v0.20.8"
include = ["assets/**/*"]

[[dependencies]]
source = "../dep-a"

[[dependencies]]
name = "named"
source = "../dep-b"
`
	got, err := parseSourceConfigTOML(contents)
	if err != nil {
		t.Fatal(err)
	}

	want := sourceConfig{
		dependencies: []string{"../dep-a", "../dep-b"},
		include:      []string{"assets/**/*"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toml config mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

// A module whose local directory dependency is declared only in dagger-module.toml
// (the format the engine prefers) must still have the dependency's directory pulled
// into the loaded workspace context. Otherwise the engine fails to resolve the dep
// with "dir module source does not contain a dagger config file".
func TestModuleSourceIncludeReadsTOMLDependencies(t *testing.T) {
	files := map[string]string{
		"app/dagger-module.toml": "name = \"app\"\n\n[[dependencies]]\nsource = \"../dep\"\n",
		"dep/dagger.json":        "{\"name\":\"dep\"}",
	}

	got, err := moduleSourceInclude(context.Background(), "app", func(_ context.Context, p string) (sourceConfig, bool, error) {
		if contents, ok := files[moduleConfigPath(p, configFilenameTOML)]; ok {
			config, err := parseSourceConfigTOML(contents)
			return config, true, err
		}
		if contents, ok := files[daggerJSONPath(p)]; ok {
			config, err := parseSourceConfig(contents)
			return config, true, err
		}
		return sourceConfig{}, false, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"app",
		"app/**",
		"app/dagger.json",
		"dep",
		"dep/**",
		"dep/dagger.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("include mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestModuleSourceIncludeReadsOnlyTargetAndLocalDependencies(t *testing.T) {
	var read []string

	got, err := moduleSourceInclude(context.Background(), "app", func(_ context.Context, p string) (sourceConfig, bool, error) {
		read = append(read, p)
		switch p {
		case "app":
			return sourceConfig{dependencies: []string{"../dep"}}, true, nil
		case "dep":
			return sourceConfig{}, true, nil
		default:
			t.Fatalf("unexpected config read: %s", p)
			return sourceConfig{}, false, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	wantRead := []string{"app", "dep"}
	if !reflect.DeepEqual(read, wantRead) {
		t.Fatalf("read paths mismatch:\n got: %#v\nwant: %#v", read, wantRead)
	}

	want := []string{
		"app",
		"app/**",
		"app/dagger.json",
		"dep",
		"dep/**",
		"dep/dagger.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("include mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
