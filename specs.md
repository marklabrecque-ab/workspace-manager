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
```

This structure is created by `workspace init` and all worktrees live under `spaces/`.

## Subcommands

The tool supports three subcommands. If the first argument is not a recognized subcommand, usage help is shown and the tool exits.

### `workspace init <git-remote-url>`

Bootstraps a new project from a git remote URL using the bare-clone worktree model.

#### Parameters

1) A required git remote URL (HTTPS or SSH)

#### Behavior

1. Extract the project name from the URL (last path component, stripped of `.git` suffix)
2. Create a project directory in the current working directory
3. `git clone --bare <url> <projectDir>/.bare`
4. Write a `.git` file containing `gitdir: .bare` (this makes git commands work from the project root)
5. Reconfigure the fetch refspec (bare clones use mirror mode by default): set `remote.origin.fetch` to `+refs/heads/*:refs/remotes/origin/*`, then `git fetch origin`
6. Detect the default branch via `git symbolic-ref refs/remotes/origin/HEAD`, falling back to checking for `main` then `master`
7. Create the `spaces/` directory
8. Create a worktree for the default branch: `git worktree add spaces/<branch> <branch>`
9. If `.ddev/config.yaml` exists in the new worktree, start DDEV and attempt a database import (same flow as `new`). Otherwise skip DDEV steps.
10. Print a summary

If any step fails, the entire project directory is removed.

### `workspace new <name> [identifier]`

Creates a new worktree + DDEV environment.

#### Parameters

1) A required string, which will create:
  - a new git worktree named by the string under `spaces/`
  - a new branch named by the string, based on the current branch checked out in the project
  - the new git worktree checks out this new branch

2) An optional string to override the DDEV identifier used when renaming the project

#### Behavior

The project root is located automatically using `git rev-parse --git-common-dir`, so this command works from anywhere inside the project (project root, any worktree, or subdirectory thereof).

The git worktree is created inside the `spaces/` directory under the project root. For example, if the project lives at `/home/mark/myproject` and the worktree name is `0001-new-task`, the worktree is created at `/home/mark/myproject/spaces/0001-new-task`.

If a `.ddev/config.yaml` is found in an existing worktree (preferring the current directory, then scanning `spaces/`), the DDEV project name is read from it and used for the new worktree. The identifier is prepended to the existing DDEV project name, separated by a dash. So `workspace new 0001-new-task` creates the worktree and the DDEV project name becomes `0001-project` (where `project` was the original DDEV name).

If no DDEV config is found, the DDEV steps are skipped entirely.

Once the worktree has been created and (if applicable) the name has been edited, `ddev start` is run in the new directory. After DDEV starts, a database import is attempted: the `tmp/db.sql.gz` file is used if it exists, otherwise the user is prompted for a path. If the user enters nothing, the DB import is skipped.

Finally, a summary of all steps is printed to stdout.

If any step fails, the script aborts immediately with a human-readable error and cleans up any partially-created resources.

### `workspace remove [name]`

Tears down a worktree + DDEV environment.

- `workspace remove <name>` — remove the named worktree (resolved as `spaces/<name>` under the project root)
- `workspace remove` — remove the worktree at the current directory

#### Behavior

1. **Find project root**: Located automatically via `git rev-parse --git-common-dir`.
2. **Determine target**: If a name is given, resolve it as `spaces/<name>` under the project root. If no name is given, use the current directory.
3. **Validate**: Confirm the target is a git worktree by checking `git worktree list --porcelain` (run from the project root). Bare repo entries are skipped. Abort if not found.
4. **Confirm**: Print what will be destroyed (worktree path, branch, DDEV project) and prompt `"Are you sure? (y/N)"`. Abort on anything other than `y`/`Y`.
5. **Delete DDEV**: If `.ddev/config.yaml` exists in the target, run `ddev delete --omit-snapshot -y` in the target worktree directory. Otherwise skip.
6. **Remove worktree**: Run `git worktree remove --force <path>` from the project root.
7. **Delete branch**: Run `git branch -D <branch-name>` from the project root to clean up the associated branch.
8. **Print summary** of what was removed.
