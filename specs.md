# Workspace helper

This is a Go script that will perform a number of helpful things I need to do often.

The idea is that when I want to work on a new task, I will run this script to create a new git worktree and workspace to allow me do independant work in a new environment. I use DDEV exclusively, so I will want to spawn a new unique DDEV instance for this new workspace too, with a predictable naming scheme.

## Project structure

The workspace manager uses a bare-clone model with worktrees nested inside a `spaces/` directory:

```
myproject/
  .bare/              <- bare git repo (object store)
  .git                <- file containing "gitdir: .bare"
  spaces/
    main/             <- worktree (default branch)
    0001-new-task/    <- worktree (feature branch)
  db/                 <- database dumps (db.sql.gz)
  files/              <- shared project files (synced to worktrees)
```

This structure is created by `workspace init` and all worktrees live under `spaces/`.

The `db/` directory holds database dumps (e.g. `db.sql.gz`) used during workspace creation. The `files/` directory holds shared project files (e.g. uploaded media) that are synced into new worktrees based on project type.

## Project type detection

The tool detects the project type by reading the `type:` field from `.ddev/config.yaml` in the main/master worktree. Supported types:

- **Drupal** — files sync to `web/sites/default/files/`, `settings.ddev.php` is updated with new DB hostname
- **WordPress** — files sync to `web/wp-content/uploads/`
- **Unsupported** — file syncing is skipped

## Subcommands

The tool supports four subcommands. If the first argument is not a recognized subcommand, usage help is shown and the tool exits.

### `workspace init <git-remote-url> [folder-name]`

Bootstraps a new project from a git remote URL using the bare-clone worktree model.

#### Parameters

1) A required git remote URL (HTTPS or SSH)
2) An optional folder name to override the default project directory name

#### Behavior

1. Determine the project directory name: use the provided folder name, or extract it from the URL (last path component, stripped of `.git` suffix)
2. Check that the directory does not already exist
3. Create the project directory
4. `git clone --bare <url> <projectDir>/.bare`
5. Write a `.git` file containing `gitdir: .bare` (this makes git commands work from the project root)
6. Reconfigure the fetch refspec (bare clones use mirror mode by default): set `remote.origin.fetch` to `+refs/heads/*:refs/remotes/origin/*`, then `git fetch origin`
7. Detect the default branch via `git symbolic-ref refs/remotes/origin/HEAD`, falling back to checking for `main` then `master`
8. Create `spaces/`, `db/`, and `files/` directories
9. Create a worktree for the default branch: `git worktree add spaces/<branch> <branch>`
10. Detect the project type from `.ddev/config.yaml` in the new worktree
11. If `.ddev/config.yaml` exists, start DDEV and attempt a database import from `db/db.sql.gz` (with fallback to prompting the user for a path). Otherwise skip DDEV steps.
12. Print a summary

If any step fails, the entire project directory is removed.

### `workspace new [--base <branch>] <name> [identifier]`

Creates a new worktree with its own DDEV environment.

#### Parameters

1) A required string, which will create:
  - a new git worktree named by the string under `spaces/`
  - a new branch named by the string, based on `origin/develop` if it exists, otherwise the current HEAD (or the branch specified by `--base`)
  - the new git worktree checks out this new branch

2) An optional string to override the DDEV identifier used when renaming the project. If omitted, defaults to the first 4 characters of the worktree name.

#### Options

- `--base <branch>` or `--base=<branch>` — Create the new branch starting from `<branch>` instead of the default. The branch must exist; if it doesn't, the command fails with an error.

#### Behavior

The project root is located automatically using `git rev-parse --git-common-dir`, so this command works from anywhere inside the project (project root, any worktree, or subdirectory thereof).

The git worktree is created inside the `spaces/` directory under the project root. For example, if the project lives at `/home/mark/myproject` and the worktree name is `0001-new-task`, the worktree is created at `/home/mark/myproject/spaces/0001-new-task`.

**DDEV project naming:**

The DDEV project name is read from the main/master worktree (which always has the original un-prefixed name). For feature branches, the identifier is prepended to the original name, separated by a dash — e.g. `workspace new 0001-new-task` creates the DDEV project `0001-projectname`.

For default branches (main/master), the DDEV project name is kept as-is (not renamed).

If no DDEV config is found in any worktree, the DDEV steps are skipped entirely.

**After creating the worktree and renaming DDEV (for non-default branches):**

1. `.ddev/config.yaml` is marked as `assume-unchanged` so the name change is never committed
2. For Drupal projects, `settings.ddev.php` is updated with the new database hostname (`ddev-<newname>-db`) and also marked as `assume-unchanged`
3. `.ddev/traefik` is removed so DDEV regenerates its Traefik config for the new project name
4. `ddev start` is run in the new worktree
5. Project files are synced from the shared `files/` directory using rsync:
   - Drupal: `files/` → `web/sites/default/files/`
   - WordPress: `files/` → `web/wp-content/uploads/`
   - Skipped for unsupported project types or if `files/` is empty/missing
6. A database import is attempted from `db/db.sql.gz`. If the file doesn't exist, the user is prompted for a path. If the user enters nothing, the import is skipped.
7. A summary of all steps is printed

If any step fails, the script aborts with a human-readable error and cleans up partially-created resources (DDEV project, worktree directory).

### `workspace remove [name]`

Tears down a worktree and its DDEV environment.

- `workspace remove <name>` — remove the named worktree (resolved as `spaces/<name>` under the project root)
- `workspace remove` — remove the worktree at the current directory

#### Behavior

1. **Find project root**: Located automatically via `git rev-parse --git-common-dir`.
2. **Determine target**: If a name is given, resolve it as `spaces/<name>` under the project root. If no name is given, use the current directory.
3. **Validate**: Confirm the target is a git worktree by checking `git worktree list --porcelain` (run from the project root). Bare repo entries are skipped. Abort if not found.
4. **Confirm**: Print what will be destroyed (worktree path, branch, DDEV project) and prompt `"Are you sure? (y/N)"`. Abort on anything other than `y`/`Y`.
5. **Delete DDEV**: If `.ddev/config.yaml` exists in the target, run `ddev delete --omit-snapshot -y` in the target worktree directory. Otherwise skip.
6. **Remove worktree**: Run `git worktree remove --force <path>` from the project root. If the worktree directory still exists after removal, it is deleted.
7. **Delete branch**: Run `git branch -D <branch-name>` from the project root to clean up the associated branch.
8. **Prune Docker build cache**: Run `docker builder prune -f` to reclaim disk space.
9. **Print summary** of what was removed.

### `workspace list` (alias: `ls`)

Lists all worktrees in the project.

#### Behavior

1. **Find project root**: Located automatically via `git rev-parse --git-common-dir`.
2. **Parse worktrees**: Runs `git worktree list --porcelain` and filters to only show worktrees under the `spaces/` directory (bare repo entries are excluded).
3. **Display**: Each worktree is printed as `<name>  (<branch>)`, or `<name>  (detached)` for detached HEAD states. Names are column-aligned.
4. If no worktrees are found, prints "No workspaces found."
