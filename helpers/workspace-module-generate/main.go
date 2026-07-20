package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dagger.io/dagger"
	toml "github.com/pelletier/go-toml"
)

const workspaceIDEnv = "WORKSPACE_ID"

type moduleSourceOptions struct {
	ref     string
	cwd     string
	local   bool
	name    string
	root    string
	before  string
	after   string
	idOut   string
	viewOut string
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	opts, err := parseModuleSourceOptions(os.Args[1:], 1)
	if err != nil {
		return err
	}

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		return err
	}
	defer client.Close()

	workspaceID, err := envString(workspaceIDEnv)
	if err != nil {
		return err
	}
	workspace := dagger.Ref[*dagger.Workspace](client, dagger.ID(workspaceID))

	if opts.viewOut != "" {
		view, err := moduleSourceWorkspaceView(ctx, workspace, opts)
		if err != nil {
			return err
		}
		if _, err := view.Export(ctx, opts.viewOut); err != nil {
			return fmt.Errorf("export module source workspace view: %w", err)
		}
		return nil
	}

	src, err := moduleSource(ctx, client, workspace, opts)
	if err != nil {
		return err
	}
	if opts.name != "" {
		src = src.WithName(opts.name)
	}
	if opts.idOut != "" {
		id, err := src.ID(ctx)
		if err != nil {
			return fmt.Errorf("resolve module source id: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(opts.idOut), 0o755); err != nil {
			return err
		}
		return os.WriteFile(opts.idOut, []byte(id), 0o644)
	}

	changes := src.GeneratedContextChangeset()
	if _, err := changes.Before().Directory(opts.root).Export(ctx, opts.before); err != nil {
		return fmt.Errorf("export generated context before directory: %w", err)
	}
	if _, err := changes.After().Directory(opts.root).Export(ctx, opts.after); err != nil {
		return fmt.Errorf("export generated context after directory: %w", err)
	}
	return nil
}

func parseModuleSourceOptions(args []string, wantPositionals int) (moduleSourceOptions, error) {
	opts, rest, err := parseOptions(args)
	if err != nil {
		return opts, err
	}
	if len(rest) != wantPositionals {
		return opts, fmt.Errorf("usage: workspace-module-generate REF [--cwd CWD] [--local] [--name NAME] [--root ROOT] [--before PATH] [--after PATH] [--id-out PATH] [--view-out PATH]")
	}
	opts.ref = rest[0]
	if opts.root == "" {
		opts.root = "."
	}
	if opts.before == "" {
		opts.before = "/before"
	}
	if opts.after == "" {
		opts.after = "/after"
	}
	return opts, nil
}

func parseOptions(args []string) (moduleSourceOptions, []string, error) {
	var opts moduleSourceOptions
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--local":
			opts.local = true
		case arg == "--cwd":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--cwd requires a value")
			}
			opts.cwd = args[i]
		case strings.HasPrefix(arg, "--cwd="):
			opts.cwd = strings.TrimPrefix(arg, "--cwd=")
		case arg == "--name":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--name requires a value")
			}
			opts.name = args[i]
		case strings.HasPrefix(arg, "--name="):
			opts.name = strings.TrimPrefix(arg, "--name=")
		case arg == "--root":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i]
		case strings.HasPrefix(arg, "--root="):
			opts.root = strings.TrimPrefix(arg, "--root=")
		case arg == "--before":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--before requires a value")
			}
			opts.before = args[i]
		case strings.HasPrefix(arg, "--before="):
			opts.before = strings.TrimPrefix(arg, "--before=")
		case arg == "--after":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--after requires a value")
			}
			opts.after = args[i]
		case strings.HasPrefix(arg, "--after="):
			opts.after = strings.TrimPrefix(arg, "--after=")
		case arg == "--id-out":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--id-out requires a value")
			}
			opts.idOut = args[i]
		case strings.HasPrefix(arg, "--id-out="):
			opts.idOut = strings.TrimPrefix(arg, "--id-out=")
		case arg == "--view-out":
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("--view-out requires a value")
			}
			opts.viewOut = args[i]
		case strings.HasPrefix(arg, "--view-out="):
			opts.viewOut = strings.TrimPrefix(arg, "--view-out=")
		case strings.HasPrefix(arg, "-"):
			return opts, nil, fmt.Errorf("unknown option: %s", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func moduleSource(
	ctx context.Context,
	client *dagger.Client,
	workspace *dagger.Workspace,
	opts moduleSourceOptions,
) (*dagger.ModuleSource, error) {
	cwd := opts.cwd
	if cwd == "" {
		var err error
		cwd, err = currentWorkspacePath(ctx, workspace)
		if err != nil {
			return nil, err
		}
	}

	candidate, err := workspacePath(cwd, opts.ref)
	if err != nil {
		return nil, err
	}

	local, err := workspaceDirectoryExists(ctx, workspace, candidate)
	if err != nil {
		return nil, err
	}
	if local {
		include, err := workspaceModuleSourceInclude(ctx, workspace, candidate)
		if err != nil {
			return nil, err
		}
		return workspace.
			Directory("/", dagger.WorkspaceDirectoryOpts{Include: include}).
			AsModuleSource(dagger.DirectoryAsModuleSourceOpts{SourceRootPath: candidate}), nil
	}
	if opts.local || mustBeLocalRef(opts.ref) {
		return nil, fmt.Errorf("local module source %q does not exist in workspace at %q", opts.ref, candidate)
	}

	return client.ModuleSource(opts.ref, dagger.ModuleSourceOpts{
		DisableFindUp: true,
	}), nil
}

func moduleSourceWorkspaceView(
	ctx context.Context,
	workspace *dagger.Workspace,
	opts moduleSourceOptions,
) (*dagger.Directory, error) {
	cwd := opts.cwd
	if cwd == "" {
		var err error
		cwd, err = currentWorkspacePath(ctx, workspace)
		if err != nil {
			return nil, err
		}
	}

	candidate, err := workspacePath(cwd, opts.ref)
	if err != nil {
		return nil, err
	}

	local, err := workspaceDirectoryExists(ctx, workspace, candidate)
	if err != nil {
		return nil, err
	}
	if !local {
		return nil, fmt.Errorf("local module source %q does not exist in workspace at %q", opts.ref, candidate)
	}

	include, err := workspaceModuleSourceInclude(ctx, workspace, candidate)
	if err != nil {
		return nil, err
	}
	return workspace.Directory("/", dagger.WorkspaceDirectoryOpts{Include: include}), nil
}

func workspaceDirectoryExists(ctx context.Context, workspace *dagger.Workspace, p string) (bool, error) {
	p, err := clean(p)
	if err != nil {
		return false, err
	}
	if p == "." {
		return true, nil
	}
	return workspace.
		Directory("/", dagger.WorkspaceDirectoryOpts{Include: []string{p, path.Join(p, "**")}}).
		Exists(ctx, p, dagger.DirectoryExistsOpts{ExpectedType: dagger.ExistsTypeDirectoryType})
}

func workspaceModuleSourceInclude(
	ctx context.Context,
	workspace *dagger.Workspace,
	modulePath string,
) ([]string, error) {
	return moduleSourceInclude(ctx, modulePath, func(ctx context.Context, p string) (sourceConfig, bool, error) {
		// Prefer the current dagger-module.toml config; fall back to the legacy
		// dagger.json. A module's own files are loaded via "**", but local
		// directory dependencies live outside the module directory, so we must
		// parse the config and recurse into them — otherwise a dependency
		// declared only in dagger-module.toml is dropped from the loaded context
		// and the engine fails with "dir module source does not contain a dagger
		// config file".
		tomlPath := moduleConfigPath(p, configFilenameTOML)
		ok, err := configFileExists(ctx, workspace, tomlPath)
		if err != nil {
			return sourceConfig{}, false, err
		}
		if ok {
			contents, err := workspace.
				Directory("/", dagger.WorkspaceDirectoryOpts{Include: []string{tomlPath}}).
				File(tomlPath).Contents(ctx)
			if err != nil {
				return sourceConfig{}, false, err
			}
			config, err := parseSourceConfigTOML(contents)
			if err != nil {
				return sourceConfig{}, true, fmt.Errorf("parse %s: %w", tomlPath, err)
			}
			return config, true, nil
		}

		configPath := daggerJSONPath(p)
		ok, err = configFileExists(ctx, workspace, configPath)
		if err != nil {
			return sourceConfig{}, false, err
		}
		if !ok {
			return sourceConfig{}, false, nil
		}
		contents, err := workspace.
			Directory("/", dagger.WorkspaceDirectoryOpts{Include: []string{configPath}}).
			File(configPath).Contents(ctx)
		if err != nil {
			return sourceConfig{}, false, err
		}
		config, err := parseSourceConfig(contents)
		if err != nil {
			return sourceConfig{}, true, fmt.Errorf("parse %s: %w", configPath, err)
		}
		return config, true, nil
	})
}

type sourceConfig struct {
	dependencies []string
	include      []string
}

func moduleSourceIncludeFromConfigs(configs map[string]sourceConfig, modulePath string) ([]string, error) {
	return moduleSourceInclude(context.Background(), modulePath, func(_ context.Context, p string) (sourceConfig, bool, error) {
		config, ok := configs[daggerJSONPath(p)]
		return config, ok, nil
	})
}

type sourceConfigReader func(context.Context, string) (sourceConfig, bool, error)

func moduleSourceInclude(ctx context.Context, modulePath string, readConfig sourceConfigReader) ([]string, error) {
	include := map[string]struct{}{}
	seen := map[string]struct{}{}
	var visit func(string) error
	visit = func(p string) error {
		p, err := clean(p)
		if err != nil {
			return err
		}
		if _, ok := seen[p]; ok {
			return nil
		}
		seen[p] = struct{}{}

		config, ok, err := readConfig(ctx, p)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("module source config (%s or dagger.json) not found in %q", configFilenameTOML, p)
		}

		if p == "." {
			include["."] = struct{}{}
			include["dagger.json"] = struct{}{}
			include["**"] = struct{}{}
		} else {
			include[p] = struct{}{}
			include[daggerJSONPath(p)] = struct{}{}
			include[path.Join(p, "**")] = struct{}{}
		}

		for _, includePath := range config.include {
			resolved, err := workspacePath(p, includePath)
			if err != nil {
				return err
			}
			include[resolved] = struct{}{}
		}

		for _, dep := range config.dependencies {
			if mustBeLocalRef(dep) {
				depPath, err := workspacePath(p, dep)
				if err != nil {
					return err
				}
				if err := visit(depPath); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(modulePath); err != nil {
		return nil, err
	}

	ordered := make([]string, 0, len(include))
	for p := range include {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func parseSourceConfig(contents string) (sourceConfig, error) {
	var config struct {
		Dependencies []json.RawMessage `json:"dependencies"`
		Include      []json.RawMessage `json:"include"`
	}
	if err := json.Unmarshal([]byte(contents), &config); err != nil {
		return sourceConfig{}, err
	}

	var parsed sourceConfig
	for _, raw := range config.Dependencies {
		var source string
		if err := json.Unmarshal(raw, &source); err == nil {
			parsed.dependencies = append(parsed.dependencies, source)
			continue
		}

		var object struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return sourceConfig{}, err
		}
		if object.Source != "" {
			parsed.dependencies = append(parsed.dependencies, object.Source)
		}
	}
	for _, raw := range config.Include {
		var includePath string
		if err := json.Unmarshal(raw, &includePath); err == nil && includePath != "" {
			parsed.include = append(parsed.include, includePath)
		}
	}
	return parsed, nil
}

// parseSourceConfigTOML reads the dependencies and include paths from a
// dagger-module.toml config.
func parseSourceConfigTOML(contents string) (sourceConfig, error) {
	var config struct {
		Dependencies []struct {
			Source string `toml:"source"`
		} `toml:"dependencies"`
		Include []string `toml:"include"`
	}
	if err := toml.Unmarshal([]byte(contents), &config); err != nil {
		return sourceConfig{}, err
	}

	var parsed sourceConfig
	for _, dep := range config.Dependencies {
		if dep.Source != "" {
			parsed.dependencies = append(parsed.dependencies, dep.Source)
		}
	}
	for _, includePath := range config.Include {
		if includePath != "" {
			parsed.include = append(parsed.include, includePath)
		}
	}
	return parsed, nil
}

const configFilenameTOML = "dagger-module.toml"

func daggerJSONPath(modulePath string) string {
	return moduleConfigPath(modulePath, "dagger.json")
}

func moduleConfigPath(modulePath, filename string) string {
	if modulePath == "." {
		return filename
	}
	return path.Join(modulePath, filename)
}

func configFileExists(ctx context.Context, workspace *dagger.Workspace, configPath string) (bool, error) {
	return workspace.
		Directory("/", dagger.WorkspaceDirectoryOpts{Include: []string{configPath}}).
		Exists(ctx, configPath, dagger.DirectoryExistsOpts{ExpectedType: dagger.ExistsTypeRegularType})
}

func workspacePath(cwd, ref string) (string, error) {
	cwd, err := clean(cwd)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(ref, "/") {
		return clean(ref)
	}
	if cwd == "." {
		return clean(ref)
	}
	return clean(path.Join(cwd, ref))
}

func currentWorkspacePath(ctx context.Context, workspace *dagger.Workspace) (string, error) {
	// Newer engines do not expose Workspace.path. Searching for "." from "."
	// returns the current workspace directory as a workspace-root-relative path.
	cwd, err := workspace.FindUp(ctx, ".", dagger.WorkspaceFindUpOpts{From: "."})
	if err != nil {
		return "", err
	}
	return clean(cwd)
}

func mustBeLocalRef(ref string) bool {
	if ref == "" {
		return false
	}
	return strings.HasPrefix(ref, "/") ||
		strings.HasPrefix(ref, ".") ||
		strings.HasPrefix(ref, "..") ||
		!strings.Contains(ref, ".")
}

func clean(p string) (string, error) {
	p = path.Clean(strings.TrimPrefix(p, "/"))
	if p == "." || p == "" {
		return ".", nil
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return p, nil
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
