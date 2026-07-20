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

const workspaceIDEnv = "WORKSPACE_ID"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("workspace-snapshot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	kind := fs.String("kind", "", "workspace path kind: file or directory")
	workspacePath := fs.String("path", "", "workspace-root-relative path")
	localPath := fs.String("local-path", "", "output-relative path")
	out := fs.String("out", "/out", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *kind != "file" && *kind != "directory" {
		return fmt.Errorf("--kind must be file or directory")
	}
	if *workspacePath == "" {
		return fmt.Errorf("--path is required")
	}
	if *localPath == "" {
		return fmt.Errorf("--local-path is required")
	}

	workspaceID, err := envString(workspaceIDEnv)
	if err != nil {
		return err
	}
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		return err
	}
	defer client.Close()

	workspace := dagger.Ref[*dagger.Workspace](client, dagger.ID(workspaceID))
	switch *kind {
	case "file":
		return exportFile(ctx, workspace, *workspacePath, *localPath, *out)
	case "directory":
		return exportDirectory(ctx, workspace, *workspacePath, *localPath, *out)
	default:
		panic("unreachable")
	}
}

func exportFile(ctx context.Context, workspace *dagger.Workspace, workspacePath, localPath, out string) error {
	workspacePath, err := cleanWorkspacePath(workspacePath)
	if err != nil {
		return err
	}
	localPath, err = cleanLocalPath(localPath)
	if err != nil {
		return err
	}
	if localPath == "." {
		return fmt.Errorf("cannot export file to output root")
	}

	src := workspace.Directory("/", dagger.WorkspaceDirectoryOpts{Include: []string{workspacePath}})
	exists, err := src.Exists(ctx, workspacePath, dagger.DirectoryExistsOpts{
		ExpectedType: dagger.ExistsTypeRegularType,
	})
	if err != nil {
		return err
	}
	if !exists {
		return os.MkdirAll(out, 0o755)
	}

	dst := filepath.Join(out, filepath.FromSlash(localPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	_, err = src.File(workspacePath).Export(ctx, dst)
	return err
}

func exportDirectory(ctx context.Context, workspace *dagger.Workspace, workspacePath, localPath, out string) error {
	workspacePath, err := cleanWorkspacePath(workspacePath)
	if err != nil {
		return err
	}
	localPath, err = cleanLocalPath(localPath)
	if err != nil {
		return err
	}

	src := workspace.Directory("/", dagger.WorkspaceDirectoryOpts{Include: includeDirectory(workspacePath)})
	exists, err := src.Exists(ctx, workspacePath, dagger.DirectoryExistsOpts{
		ExpectedType: dagger.ExistsTypeDirectoryType,
	})
	if err != nil {
		return err
	}
	if !exists {
		return os.MkdirAll(out, 0o755)
	}

	dst := out
	if localPath != "." {
		dst = filepath.Join(out, filepath.FromSlash(localPath))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
	}
	_, err = src.Directory(workspacePath).Export(ctx, dst)
	return err
}

func includeDirectory(p string) []string {
	if p == "." {
		return []string{"**", "**/.*/**"}
	}
	return []string{p, path.Join(p, "**"), path.Join(p, "**/.*/**")}
}

func cleanWorkspacePath(p string) (string, error) {
	p = path.Clean(strings.TrimPrefix(p, "/"))
	if p == "." || p == "" {
		return ".", nil
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("workspace path escapes workspace: %s", p)
	}
	return p, nil
}

func cleanLocalPath(p string) (string, error) {
	p = path.Clean(strings.TrimPrefix(p, "/"))
	if p == "" {
		return ".", nil
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("local path escapes output: %s", p)
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
