# Workspace Manager

A CLI tool for managing git worktree-based workspaces. It uses a bare-clone model where all worktrees live inside a `spaces/` directory, keeping your project organized.

## Project Structure

```
myproject/
  .bare/              <- bare git repo (object store)
  .git                <- file containing "gitdir: .bare"
  spaces/
    main/             <- worktree (default branch)
    0001-new-task/    <- worktree (feature branch)
```

## Commands

### `workspace init <git-remote-url> [folder-name]`

Bootstrap a new project from a git remote:

```
workspace init git@github.com:user/project.git
workspace init git@github.com:user/project.git myproject   # custom folder name
```

This clones the repo as a bare repository, sets up the `spaces/` directory structure, and creates a worktree for the default branch. If the project uses DDEV, it will be started automatically.

### `workspace new [--base <branch>] <name> [identifier]`

Create a new worktree (and optionally a DDEV environment):

```
workspace new 0001-new-task
workspace new 0001-new-task t1              # custom DDEV identifier
workspace new --base develop 0001-new-task  # branch off develop
```

Works from anywhere inside the project. Creates a new branch and worktree under `spaces/`. Use `--base` to branch from a specific branch instead of the current HEAD. If DDEV is configured, the project is cloned with a unique name and started.

### `workspace remove [name]`

Remove a worktree (and its DDEV environment if present):

```
workspace remove 0001-new-task     # remove by name
workspace remove                   # remove current directory's worktree
```

## Compile

Requirements: Go v1.21+

```
go build -o workspace main.go
```

## Installation

Symlink the compiled `workspace` binary into your `$PATH`, or add this folder to your `$PATH` directly.
