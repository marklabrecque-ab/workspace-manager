# Workspace helper

This is a Go script that will perform a number of helpful things I need to do often.

The idea is that when I want to work on a new task, I will run this script to create a new git worktree and workspace to allow me do independant work in a new environment. I use DDEV exclusively, so I will want to spawn a new unique DDEV instance for this new workspace too, with a predictable naming scheme.

## Parameters

This script should accept a couple parameters:

1) It should accept a string, which will create a couple things with that string:
  - a new git worktree named by the string
  - a new branch named by the string. This new branch will be based on the current branch checked out in the project. The new git worktree just created should check out this new branch
  - this parameter is required and the script should fail if it is not provided

2) Optionally, the user should be able to provide another string, which will override what the DDEV identifier will be when renaming the project

## Functionality

When creating a new git worktree, the script should also change the DDEV project name to be able to give it a unique identifier. By default, it should take the first 4 characters of the first parameter accepted by the string. If a second parameter is provided, use that instead. It should take the existing DDEV project name and prefix it with the identifier, separated by a dash. So if I run the command `workspace 0001-new-task` it should create the git worktree within a folder named 0001-new-task and the DDEV project name should be changed to 0001-project (where project was the original DDEV name).

Once the gitwork tree has been created and the name has been edited, change directory into this folder and then run `ddev start` to start the environment.
Once the ddev start command has finished, I'd like to import a database into the new environment. Look inside the tmp folder for a file called db.sql.gz and use that if it exists. If it doesn't exist, prompt the user for the location of the DB backup and use that. If the user enters nothing at this prompt, skip the DB import step.

Finally, summarize all of the steps taken by this script in stdout, for the user. Be clear that the run succeeded in this report.

If at any time you run into issues while running the steps of this script, fail immediately and give the user a human-readable explanation as to how it failed.


  
