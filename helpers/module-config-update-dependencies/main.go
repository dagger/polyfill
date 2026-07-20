package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"dagger.io/dagger"
)

const (
	workspaceIDEnv      = "WORKSPACE_ID"
	configContentsEnv   = "MODULE_CONFIG_CONTENTS"
	updatesJSONEnv      = "MODULE_CONFIG_UPDATES_JSON"
	defaultGitHeadValue = "ref: refs/heads/main\n"
	mockRoot            = "/mock"
)

type configDependency struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Pin    string `json:"pin,omitempty"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("module-config-update-dependencies", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	modulePath := fs.String("path", "", "workspace-root-relative module path")
	outPath := fs.String("out", "/dependencies.json", "updated dependencies JSON output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *modulePath == "" {
		return fmt.Errorf("--path is required")
	}

	workspaceID, err := envString(workspaceIDEnv)
	if err != nil {
		return err
	}
	contents, err := envString(configContentsEnv)
	if err != nil {
		return err
	}
	updates, err := updatesFromEnv()
	if err != nil {
		return err
	}

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		return err
	}
	defer client.Close()

	workspace := dagger.Ref[*dagger.Workspace](client, dagger.ID(workspaceID))
	dependencies, err := updatedRemoteDependencies(ctx, client, workspace, *modulePath, contents, updates)
	if err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(dependencies, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dependencies: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(*outPath, append(encoded, '\n'), 0o644)
}

func updatedRemoteDependencies(
	ctx context.Context,
	client *dagger.Client,
	workspace *dagger.Workspace,
	modulePath string,
	contents string,
	updates []string,
) ([]configDependency, error) {
	modulePath, err := cleanModulePath(modulePath)
	if err != nil {
		return nil, err
	}

	mock := workspace.
		Directory("/", dagger.WorkspaceDirectoryOpts{Include: []string{"**/dagger.json"}}).
		WithNewFile(daggerJSONPath(modulePath), contents).
		WithNewFile(".git/HEAD", defaultGitHeadValue)
	if _, err := mock.Export(ctx, mockRoot); err != nil {
		return nil, fmt.Errorf("export mock workspace: %w", err)
	}

	dependencies, err := workspaceModuleSource(client, modulePath).WithUpdateDependencies(updates).Dependencies(ctx)
	if err != nil {
		return nil, err
	}

	updated := []configDependency{}
	for _, dependency := range dependencies {
		fragment, ok, err := remoteDependency(ctx, &dependency)
		if err != nil {
			return nil, err
		}
		if ok {
			updated = append(updated, fragment)
		}
	}
	return updated, nil
}

func workspaceModuleSource(client *dagger.Client, modulePath string) *dagger.ModuleSource {
	return client.ModuleSource(mockSourcePath(modulePath), dagger.ModuleSourceOpts{
		DisableFindUp: true,
		RequireKind:   dagger.ModuleSourceKindLocalSource,
	})
}

func remoteDependency(ctx context.Context, dependency *dagger.ModuleSource) (configDependency, bool, error) {
	kind, err := dependency.Kind(ctx)
	if err != nil {
		return configDependency{}, false, err
	}

	switch kind {
	case dagger.ModuleSourceKindLocalSource:
		return configDependency{}, false, nil
	case dagger.ModuleSourceKindGitSource:
	default:
		return configDependency{}, false, fmt.Errorf("unsupported dependency kind in update response: %s", kind)
	}

	name, err := dependency.ModuleName(ctx)
	if err != nil {
		return configDependency{}, false, err
	}
	source, err := dependency.AsString(ctx)
	if err != nil {
		return configDependency{}, false, err
	}
	pin, err := dependency.Pin(ctx)
	if err != nil {
		return configDependency{}, false, err
	}
	return configDependency{Name: name, Source: source, Pin: pin}, true, nil
}

func updatesFromEnv() ([]string, error) {
	raw := os.Getenv(updatesJSONEnv)
	if raw == "" {
		return nil, nil
	}

	var updates []string
	if err := json.Unmarshal([]byte(raw), &updates); err != nil {
		return nil, fmt.Errorf("decode %s: %w", updatesJSONEnv, err)
	}
	return updates, nil
}

func envString(name string) (string, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return "", fmt.Errorf("%s is not set", name)
	}

	var decoded string
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return decoded, nil
	}
	return raw, nil
}

func cleanModulePath(p string) (string, error) {
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("module path must be relative: %s", p)
	}

	p = path.Clean(p)
	if p == "" {
		return ".", nil
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("module path escapes workspace: %s", p)
	}
	return p, nil
}

func daggerJSONPath(modulePath string) string {
	if modulePath == "." {
		return "dagger.json"
	}
	return path.Join(modulePath, "dagger.json")
}

func mockSourcePath(modulePath string) string {
	if modulePath == "." {
		return mockRoot
	}
	return path.Join(mockRoot, modulePath)
}
