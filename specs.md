# Workspace helper

This is a Go script that will perform a number of helpful things I need to do often.

The idea is that when I want to work on a new task, I will run this script to create a new git worktree and workspace to allow me do independant work in a new environment. I use DDEV exclusively, so I will want to spawn a new unique DDEV instance for this new workspace too, with a predictable naming scheme.

## Subcommands

The tool supports two subcommands. If the first argument is not a recognized subcommand, usage help is shown and the tool exits.

### `workspace new <name> [identifier]`

Creates a new worktree + DDEV environment.

#### Parameters

1) A required string, which will create:
  - a new git worktree named by the string
  - a new branch named by the string, based on the current branch checked out in the project
  - the new git worktree checks out this new branch

2) An optional string to override the DDEV identifier used when renaming the project

#### Behavior

The git worktree is created as a sibling directory to the current project, not nested inside it. For example, if the project lives at `/home/mark/myproject` and the worktree name is `0001-new-task`, the worktree is created at `/home/mark/0001-new-task` (i.e. `../<worktree-name>` relative to the project root).

The DDEV project name is changed to give it a unique identifier. By default, the first 4 characters of the name are used. If a second parameter is provided, that is used instead. The identifier is prepended to the existing DDEV project name, separated by a dash. So `workspace 0001-new-task` creates the worktree in a folder named `0001-new-task` and the DDEV project name becomes `0001-project` (where `project` was the original DDEV name).

Once the worktree has been created and the name has been edited, `ddev start` is run in the new directory. After DDEV starts, a database import is attempted: the `tmp/db.sql.gz` file is used if it exists, otherwise the user is prompted for a path. If the user enters nothing, the DB import is skipped.

Finally, a summary of all steps is printed to stdout.

If any step fails, the script aborts immediately with a human-readable error and cleans up any partially-created resources.

### `workspace remove [name]`

Tears down a worktree + DDEV environment.

- `workspace remove <name>` — remove the named worktree (resolved as `../<name>` relative to cwd)
- `workspace remove` — remove the worktree at the current directory

#### Behavior

1. **Determine target**: If a name is given, resolve it as a sibling directory (`../<name>` relative to cwd). If no name is given, use the current directory.
2. **Validate**: Confirm the target is a git worktree by checking `git worktree list --porcelain`. Abort if not.
3. **Confirm**: Print what will be destroyed (worktree path, branch, DDEV project) and prompt `"Are you sure? (y/N)"`. Abort on anything other than `y`/`Y`.
4. **Delete DDEV**: Run `ddev delete --omit-snapshot -y` in the target worktree directory.
5. **Remove worktree**: Run `git worktree remove --force <path>` from outside the worktree.
6. **Delete branch**: Run `git branch -D <branch-name>` to clean up the associated branch.
7. **Print summary** of what was removed.
