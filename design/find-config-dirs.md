# `findConfigDirs`

Polyfill for [dagger/dagger#13688](https://github.com/dagger/dagger/issues/13688): a
CWD-aware way for modules to discover config directories, so every official module
stops inventing its own. Reference implementations:
[dagger/typescript-sdk#11](https://github.com/dagger/typescript-sdk/pull/11),
[dagger/deno#4](https://github.com/dagger/deno/pull/4). Both copy-paste the same
private helper — this hosts it once so they can depend on it instead.

## API

On `PolyfillWorkspace` (`workspace.dang`):

```dang
pub findConfigDirs(
  filenames: [String!]!,
  exclude: [String!]! = [],
): [String!]!
```

- `filenames` — config basenames to match, e.g. `["deno.json", "deno.jsonc"]`.
- `exclude` — glob patterns pruning the walk-down, e.g. `["**/node_modules/**"]`.
  Needed because vendored trees are full of false positives.

Returns **directories**, deduped, each usable as-is with `ws.directory(path)`.

## Semantics

Two searches, both anchored at the client's CWD, never the workspace root:

| search | yields | count | path form |
| --- | --- | --- | --- |
| walk-down | every dir at or below the CWD holding a config file | 0-N | `.`, `sub/dir` |
| find-up | nearest **strict** ancestor holding a config file | 0-1 | `..`, `../..` |

`ws.findUp` counts the CWD itself as a hit; that case is dropped because walk-down
already covers it. So find-up only ever contributes a directory the caller lives
*under*, and it stops at the first one — a farther-up project never shadows a
closer one.

**Every path is CWD-relative**, so results read the way a shell user would write
them and all resolve through `ws.directory(path)` unchanged. A `..` prefix is
exactly the "this is an ancestor, not in my cone" signal consumers need — this is
what `deno.dang`'s `inCone` tested before PR #4 switched it to a leading `/`.

This diverges from the two reference PRs, which return the find-up hit as a
workspace-absolute path (`/a/b`) and leave walk-down hits relative. Same
information, friendlier form; if we upstream this into core, `inCone` becomes
`hasPrefix("..")` again and `typescript-sdk`'s `moduleRelPath` resolves `..`
segments instead of stripping a leading `/`.

`cwd` comes from the existing `PolyfillWorkspace.cwd` field, not `ws.cwd` — the
polyfill already computes it via `PolyfillWorkspaceCwd` for engines that dropped
`Workspace.path`.

## Worked example

Fixture:

```
deno.json
dir/coucou.txt
sub/deno.json
sub/dir/sub/deno.json
sub/toto/a.txt
```

| CWD | result | why |
| --- | --- | --- |
| `.` | `.`, `sub`, `sub/dir/sub` | walk-down finds all three; find-up hits `/` = CWD, dropped |
| `sub` | `.`, `dir/sub` | walk-down; find-up hits `sub` = CWD, dropped — the root project is *not* returned |
| `dir` | `..` | walk-down finds nothing; find-up reaches the root, a strict ancestor |

## Implementation sketch

Three private helpers on `PolyfillWorkspace`, lifted verbatim from the PRs plus
`exclude`:

- `parentConfigDir(filenames)` — `filenames.reduce(null) { acc, name => acc ?? ws.findUp(name, ".") }`,
  `dirOf` it, drop if it equals the CWD, else convert to `..` form.
- `descendantConfigDirs(filenames, exclude)` — `ws.directory(".", include: filenames.map { "**/" + it }, exclude: exclude)`,
  then `glob("**/" + name)` per filename, `dirOf` each, `uniq`.
- `dirOf(path)` — `"/a/b/x.json" -> "/a/b"`, `"x.json" -> "."`, `"/x.json" -> "/"`.

`findUp` returns a workspace-absolute path, so the ancestor needs converting. It
is always a strict prefix of `cwd`, so the number of `..` segments is just the
depth difference:

```dang
cwd.split("/").drop(ancestorDepth).map { _ => ".." }.join("/")
```

(`cwd = "dir"`, ancestor `.` at depth 0 → `".."`; `cwd = "a/b/c"`, ancestor `a`
at depth 1 → `"../.."`.) If `drop` isn't in the stdlib I'll fall back to a small
recursive helper like the pre-#4 `collectAncestors`.

## Tests

Fixtures under `.dagger/modules/e2e/fixtures/find-config-dirs/`, mirroring the
tree above plus a `sub/node_modules/pkg/deno.json` for the exclude case. Checks in
`.dagger/modules/e2e/main.dang` load them with
`currentModule.source.directory("fixtures/find-config-dirs").asWorkspace(cwd: ...)`
— same pattern as `dagger/deno#4`.

Config files are named `deno.json`, not `dagger.json`, so the fixtures aren't
mistaken for real modules.

| check | asserts |
| --- | --- |
| `findConfigDirsRootCheck` | from `.` → `[".", "sub", "sub/dir/sub"]`, CWD not duplicated by find-up |
| `findConfigDirsSubdirCheck` | from `sub` → `[".", "dir/sub"]`, root project absent |
| `findConfigDirsAncestorCheck` | from `dir` → `[".."]`, and `ws.directory("..")` resolves to the root project |
| `findConfigDirsExcludeCheck` | `exclude: ["**/node_modules/**"]` prunes the vendored hit; without it, it appears |
| `findConfigDirsMultiFilenameCheck` | `["deno.json", "deno.jsonc"]` matches either and dedupes a dir holding both |

No checks for other `PolyfillWorkspace` methods, per your scope.
