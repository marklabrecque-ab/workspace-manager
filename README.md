# Workspace Manager

A CLI tool for managing git worktree-based workspaces. It uses a bare-clone model where all worktrees live inside a `spaces/` directory, keeping your project organized.

Supports Drupal and WordPress projects with automatic DDEV integration.

## Project Structure

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

## Commands

### `workspace init <git-remote-url> [folder-name]`

Bootstrap a new project from a git remote:

```
workspace init git@github.com:user/project.git
workspace init git@github.com:user/project.git myproject   # custom folder name
```

This clones the repo as a bare repository, sets up the `spaces/`, `db/`, and `files/` directory structure, and creates a worktree for the default branch. The project type (Drupal or WordPress) is detected from `.ddev/config.yaml`. If the project uses DDEV, it will be started automatically and a database import from `db/db.sql.gz` is attempted.

### `workspace new [--base <branch>] <name> [identifier]`

Create a new worktree with its own DDEV environment:

```
workspace new 0001-new-task
workspace new 0001-new-task t1              # custom DDEV identifier
workspace new --base develop 0001-new-task  # branch off develop
```

Works from anywhere inside the project. Creates a new branch and worktree under `spaces/`. If `--base` is not specified, it defaults to `origin/develop` if that branch exists, otherwise the current HEAD.

For DDEV projects, the new worktree gets a uniquely-named DDEV instance (e.g. `0001-projectname`). Project files from the shared `files/` directory are synced into the worktree (to `web/sites/default/files/` for Drupal, or `web/wp-content/uploads/` for WordPress). For Drupal projects, `settings.ddev.php` is updated with the new database hostname.

A database import from `db/db.sql.gz` is attempted after DDEV starts. If the file doesn't exist, you'll be prompted for a path.

### `workspace remove [name]`

Remove a worktree and its DDEV environment:

```
workspace remove 0001-new-task     # remove by name
workspace remove                   # remove current directory's worktree
```

Shows what will be destroyed and asks for confirmation. Deletes the DDEV project, removes the git worktree and branch, and prunes the Docker build cache to free disk space.

### `workspace list`

List all worktrees in the project:

```
workspace list
workspace ls        # alias
```

Shows each worktree name and its checked-out branch.

## Compile

Requirements: Go v1.21+

```
go build -o workspace main.go
```

## Installation

Symlink the compiled `workspace` binary into your `$PATH`, or add this folder to your `$PATH` directly.
