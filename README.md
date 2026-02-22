# Workspace Manager

This script allows a user to setup a new Git worktree inside a project folder. It will base the worktree on the current branch. It requires one argument, which is the branch name that will be checked out in the worktree.

## Operations

This script does the following (in this order):

1) creates a new git worktree based on the branch name provided in the first argument.
2) alter the DDEV config name value to be unique. Without a second argument, it will take the first 4 characters of your new branch name.
3) It will look for tmp/db.sql.gz and if it finds it, will automatically import it into your DDEV environment. If it can't find it, it will prompt you to provide a DB backup file location or skip this.

## Compile

Requirements: - Go v1.21+

Compilation: go build -o workspace main.go (or just go build)

# Installation
Be sure to symlink the binary produced (workspace) into your $PATH. Or just add this folder to your $PATH directly.