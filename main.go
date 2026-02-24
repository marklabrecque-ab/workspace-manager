package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type StepResult struct {
	Description string
	Detail      string
}

type cleanupState struct {
	worktreePath    string
	projectRoot     string
	worktreeCreated bool
	ddevStarted     bool
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "init":
		cmdInit(args[1:])
	case "new":
		cmdNewFromArgs(args[1:])
	case "remove":
		cmdRemove(args[1:])
	case "list", "ls":
		cmdList()
	case "--help", "-h":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: workspace <command> [arguments]

Commands:
  init <url> [folder]     Clone a repo into a bare-clone workspace structure
  new [--base <branch>] <name> [identifier]
                           Create a new worktree + DDEV environment
  remove [name]            Remove a worktree + DDEV environment
  list                     List all workspaces

Examples:
  workspace init git@github.com:user/project.git
  workspace init git@github.com:user/project.git myproject
  workspace new 0001-new-task
  workspace new 0001-new-task t1              (custom DDEV identifier)
  workspace new --base develop 0001-new-task  (branch off develop)
  workspace remove 0001-new-task     (remove by name)
  workspace remove                   (remove current directory's worktree)
`)
}

// findProjectRoot locates the project root from anywhere inside the project
// (worktree, project root, etc.) by finding the shared git directory.
func findProjectRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}

	gitCommonDir := strings.TrimSpace(string(out))

	// Resolve to absolute path if relative
	if !filepath.IsAbs(gitCommonDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not get working directory: %w", err)
		}
		gitCommonDir = filepath.Join(cwd, gitCommonDir)
	}

	gitCommonDir, err = filepath.Abs(gitCommonDir)
	if err != nil {
		return "", fmt.Errorf("could not resolve path: %w", err)
	}

	projectRoot := filepath.Dir(gitCommonDir)

	// Validate that .bare or .git exists at project root
	if _, err := os.Stat(filepath.Join(projectRoot, ".bare")); err == nil {
		return projectRoot, nil
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".git")); err == nil {
		return projectRoot, nil
	}

	return "", fmt.Errorf("could not find project root (no .bare or .git at %s)", projectRoot)
}

// extractProjectName extracts the project name from a git remote URL.
func extractProjectName(remoteURL string) string {
	remoteURL = strings.TrimRight(remoteURL, "/")
	name := filepath.Base(remoteURL)
	name = strings.TrimSuffix(name, ".git")
	return name
}

func cmdInit(args []string) {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Error: expected 1 or 2 arguments, got %d\n", len(args))
		fmt.Fprintf(os.Stderr, "Usage: workspace init <git-remote-url> [folder-name]\n")
		os.Exit(1)
	}

	remoteURL := args[0]
	var projectName string
	if len(args) == 2 {
		projectName = args[1]
	} else {
		projectName = extractProjectName(remoteURL)
	}
	if projectName == "" {
		fmt.Fprintf(os.Stderr, "Error: could not determine project name from URL: %s\n", remoteURL)
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	projectDir := filepath.Join(cwd, projectName)

	// Check if project directory already exists
	if _, err := os.Stat(projectDir); err == nil {
		fmt.Fprintf(os.Stderr, "Error: directory already exists: %s\n", projectDir)
		os.Exit(1)
	}

	var steps []StepResult

	// Step 1: Create project directory
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating project directory: %v\n", err)
		os.Exit(1)
	}

	// Step 2: Bare clone
	fmt.Println("--- Cloning repository (bare) ---")
	barePath := filepath.Join(projectDir, ".bare")
	cloneCmd := exec.Command("git", "clone", "--bare", remoteURL, barePath)
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error cloning repository: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Cloned repository (bare)",
		Detail:      barePath,
	})

	// Step 3: Write .git file
	gitFilePath := filepath.Join(projectDir, ".git")
	if err := os.WriteFile(gitFilePath, []byte("gitdir: .bare\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing .git file: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Created .git file",
		Detail:      gitFilePath,
	})

	// Step 4: Reconfigure fetch refspec
	configCmd := exec.Command("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = projectDir
	if err := configCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error configuring fetch refspec: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}

	fmt.Println("\n--- Fetching branches ---")
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = projectDir
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching from origin: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Configured fetch refspec",
		Detail:      "Fetched all branches",
	})

	// Step 5: Detect default branch
	defaultBranch := detectDefaultBranch(projectDir)
	if defaultBranch == "" {
		fmt.Fprintf(os.Stderr, "Error: could not detect default branch\n")
		cleanupInit(projectDir)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Default branch",
		Detail:      defaultBranch,
	})

	// Step 6: Create spaces/ and db/ directories, then first worktree
	spacesDir := filepath.Join(projectDir, "spaces")
	if err := os.MkdirAll(spacesDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating spaces directory: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}
	dbDir := filepath.Join(projectDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating db directory: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}

	fmt.Println("\n--- Creating worktree ---")
	wtPath := filepath.Join("spaces", defaultBranch)
	wtCmd := exec.Command("git", "worktree", "add", wtPath, defaultBranch)
	wtCmd.Dir = projectDir
	wtCmd.Stdout = os.Stdout
	wtCmd.Stderr = os.Stderr
	if err := wtCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating worktree: %v\n", err)
		cleanupInit(projectDir)
		os.Exit(1)
	}
	worktreeFullPath := filepath.Join(projectDir, "spaces", defaultBranch)
	steps = append(steps, StepResult{
		Description: "Created worktree",
		Detail:      worktreeFullPath,
	})

	// Step 7: Check for DDEV and optionally set it up
	ddevConfig := filepath.Join(worktreeFullPath, ".ddev", "config.yaml")
	if _, err := os.Stat(ddevConfig); err == nil {
		fmt.Println("\n--- Starting DDEV ---")
		if err := runCommandLive(worktreeFullPath, "ddev", "start"); err != nil {
			fmt.Fprintf(os.Stderr, "\nWarning: failed to start DDEV: %v\n", err)
			steps = append(steps, StepResult{
				Description: "DDEV",
				Detail:      fmt.Sprintf("Failed to start: %v", err),
			})
		} else {
			steps = append(steps, StepResult{
				Description: "DDEV",
				Detail:      "Started",
			})

			dbDetail, err := handleDBImport(worktreeFullPath, projectDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nWarning: failed to import database: %v\n", err)
				steps = append(steps, StepResult{
					Description: "Database",
					Detail:      fmt.Sprintf("Failed: %v", err),
				})
			} else {
				steps = append(steps, StepResult{
					Description: "Database",
					Detail:      dbDetail,
				})
			}
		}
	} else {
		steps = append(steps, StepResult{
			Description: "DDEV",
			Detail:      "Skipped (no .ddev/config.yaml found)",
		})
	}

	// Done
	fmt.Println()
	printSummary(steps)
}

func detectDefaultBranch(projectDir string) string {
	// Try symbolic-ref first
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		branch := strings.TrimPrefix(ref, "refs/remotes/origin/")
		if branch != ref {
			return branch
		}
	}

	// Fall back to checking for main, then master
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/"+branch)
		cmd.Dir = projectDir
		if err := cmd.Run(); err == nil {
			return branch
		}
	}

	return ""
}

func cleanupInit(projectDir string) {
	fmt.Fprintf(os.Stderr, "\n--- Cleaning up ---\n")
	fmt.Fprintf(os.Stderr, "Removing project directory %s...\n", projectDir)
	if err := os.RemoveAll(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove project directory: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "Cleanup complete.\n")
}

func cmdList() {
	projectRoot, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing worktrees: %v\n", err)
		os.Exit(1)
	}

	spacesDir := filepath.Join(projectRoot, "spaces")

	type workspace struct {
		name   string
		branch string
		path   string
	}

	var workspaces []workspace
	var currentPath string
	var currentBranch string
	isBare := false

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
			currentBranch = ""
			isBare = false
		} else if line == "bare" {
			isBare = true
		} else if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			currentBranch = strings.TrimPrefix(ref, "refs/heads/")
		} else if line == "" && currentPath != "" {
			if !isBare && strings.HasPrefix(currentPath, spacesDir+string(filepath.Separator)) {
				name := strings.TrimPrefix(currentPath, spacesDir+string(filepath.Separator))
				workspaces = append(workspaces, workspace{
					name:   name,
					branch: currentBranch,
					path:   currentPath,
				})
			}
			currentPath = ""
			currentBranch = ""
			isBare = false
		}
	}
	// Handle last entry (porcelain output may not end with a blank line)
	if currentPath != "" && !isBare && strings.HasPrefix(currentPath, spacesDir+string(filepath.Separator)) {
		name := strings.TrimPrefix(currentPath, spacesDir+string(filepath.Separator))
		workspaces = append(workspaces, workspace{
			name:   name,
			branch: currentBranch,
			path:   currentPath,
		})
	}

	if len(workspaces) == 0 {
		fmt.Println("No workspaces found.")
		return
	}

	// Find the longest name for alignment
	maxName := 0
	for _, ws := range workspaces {
		if len(ws.name) > maxName {
			maxName = len(ws.name)
		}
	}

	for _, ws := range workspaces {
		if ws.branch != "" {
			fmt.Printf("  %-*s  (%s)\n", maxName, ws.name, ws.branch)
		} else {
			fmt.Printf("  %-*s  (detached)\n", maxName, ws.name)
		}
	}
}

func cmdNewFromArgs(args []string) {
	var baseBranch string
	var positional []string

	// Parse flags
	for i := 0; i < len(args); i++ {
		if args[i] == "--base" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --base requires a branch name\n")
				os.Exit(1)
			}
			baseBranch = args[i+1]
			i++ // skip the value
		} else if strings.HasPrefix(args[i], "--base=") {
			baseBranch = strings.TrimPrefix(args[i], "--base=")
		} else {
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 1 || len(positional) > 2 {
		fmt.Fprintf(os.Stderr, "Error: expected 1 or 2 positional arguments, got %d\n", len(positional))
		fmt.Fprintf(os.Stderr, "Usage: workspace new [--base <branch>] <worktree-name> [identifier]\n")
		os.Exit(1)
	}

	worktreeName := positional[0]
	if worktreeName == "" {
		fmt.Fprintf(os.Stderr, "Error: worktree name cannot be empty\n")
		os.Exit(1)
	}

	var identifier string
	if len(positional) == 2 {
		identifier = positional[1]
	} else {
		if len(worktreeName) < 4 {
			identifier = worktreeName
		} else {
			identifier = worktreeName[:4]
		}
	}

	cmdNew(worktreeName, identifier, baseBranch)
}

func cmdNew(worktreeName, identifier, baseBranch string) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Validate base branch exists if specified
	if baseBranch != "" {
		cmd := exec.Command("git", "rev-parse", "--verify", baseBranch)
		cmd.Dir = projectRoot
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: branch %q does not exist\n", baseBranch)
			os.Exit(1)
		}
	}

	// Default to origin/develop if it exists and no base was specified
	if baseBranch == "" {
		cmd := exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/develop")
		cmd.Dir = projectRoot
		if cmd.Run() == nil {
			baseBranch = "origin/develop"
		}
	}

	worktreePath := filepath.Join(projectRoot, "spaces", worktreeName)
	state := &cleanupState{worktreePath: worktreePath, projectRoot: projectRoot}
	var steps []StepResult

	// Step 1: Read current DDEV project name (from any existing worktree)
	originalName, err := findDDEVProjectName(projectRoot)
	hasDDEV := err == nil
	if hasDDEV {
		steps = append(steps, StepResult{
			Description: "Read DDEV project name",
			Detail:      originalName,
		})
	}

	// Step 2: Create git worktree
	err = createWorktree(projectRoot, worktreeName, baseBranch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating worktree: %v\n", err)
		cleanup(state)
		os.Exit(1)
	}
	state.worktreeCreated = true
	steps = append(steps, StepResult{
		Description: "Created git worktree",
		Detail:      worktreeName,
	})

	if !hasDDEV {
		steps = append(steps, StepResult{
			Description: "DDEV",
			Detail:      "Skipped (no .ddev/config.yaml found in any worktree)",
		})
		fmt.Println()
		printSummary(steps)
		return
	}

	// Step 3: Rename DDEV project (skip for main/master — keep default name)
	isDefaultBranch := worktreeName == "main" || worktreeName == "master"
	ddevName := originalName
	if !isDefaultBranch {
		ddevName = identifier + "-" + originalName
		err = renameDDEVProject(worktreePath, identifier, originalName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error renaming DDEV project: %v\n", err)
			cleanup(state)
			os.Exit(1)
		}
		steps = append(steps, StepResult{
			Description: "Renamed DDEV project",
			Detail:      ddevName,
		})

		// Step 3b: Update settings.ddev.php with new DB host
		settingsPath := filepath.Join(worktreePath, "web", "sites", "default", "settings.ddev.php")
		if _, statErr := os.Stat(settingsPath); statErr == nil {
			err = updateSettingsDdevPHP(worktreePath, ddevName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error updating settings.ddev.php: %v\n", err)
				cleanup(state)
				os.Exit(1)
			}
			steps = append(steps, StepResult{
				Description: "Updated settings.ddev.php",
				Detail:      "DB host set to ddev-" + ddevName + "-db",
			})
		}
	} else {
		steps = append(steps, StepResult{
			Description: "DDEV project name",
			Detail:      originalName + " (kept default)",
		})
	}

	// Step 4: Start DDEV
	fmt.Println("\n--- Starting DDEV ---")
	err = runCommandLive(worktreePath, "ddev", "start")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError starting DDEV: %v\n", err)
		cleanup(state)
		os.Exit(1)
	}
	state.ddevStarted = true
	steps = append(steps, StepResult{
		Description: "Started DDEV",
		Detail:      ddevName,
	})

	// Step 5: Handle DB import
	dbDetail, err := handleDBImport(worktreePath, projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError importing database: %v\n", err)
		cleanup(state)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Database",
		Detail:      dbDetail,
	})

	// Done
	fmt.Println()
	printSummary(steps)
}

// findDDEVProjectName reads the DDEV project name from the main/master
// worktree, which always has the original (un-prefixed) name.
func findDDEVProjectName(projectRoot string) (string, error) {
	spacesDir := filepath.Join(projectRoot, "spaces")

	// Check main/master first — these keep the original DDEV name
	for _, branch := range []string{"main", "master"} {
		dir := filepath.Join(spacesDir, branch)
		if name, err := getDDEVProjectName(dir); err == nil {
			return name, nil
		}
	}

	return "", fmt.Errorf("no DDEV config found in main or master worktree")
}

func cmdRemove(args []string) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Determine target directory
	var targetPath string
	if len(args) > 0 && args[0] != "" {
		targetPath = filepath.Join(projectRoot, "spaces", args[0])
	} else {
		targetPath, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
			os.Exit(1)
		}
	}

	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}
	targetPath, err = filepath.EvalSymlinks(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Validate it's a git worktree
	branchName, err := validateWorktree(targetPath, projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Confirmation prompt
	fmt.Println("The following will be destroyed:")
	fmt.Printf("  Worktree:  %s\n", targetPath)
	fmt.Printf("  Branch:    %s\n", branchName)
	fmt.Printf("  DDEV project in that worktree (if any)\n")
	fmt.Print("\nAre you sure? (y/N) ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}
	input = strings.TrimSpace(input)
	if input != "y" && input != "Y" {
		fmt.Println("Aborted.")
		return
	}

	var steps []StepResult

	// Step 1: Delete DDEV (if present)
	ddevConfig := filepath.Join(targetPath, ".ddev", "config.yaml")
	if _, err := os.Stat(ddevConfig); err == nil {
		fmt.Println("\n--- Deleting DDEV project ---")
		ddevCmd := exec.Command("ddev", "delete", "--omit-snapshot", "-y")
		ddevCmd.Dir = targetPath
		ddevCmd.Stdout = os.Stdout
		ddevCmd.Stderr = os.Stderr
		if err := ddevCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to delete DDEV project: %v\n", err)
			steps = append(steps, StepResult{
				Description: "DDEV project",
				Detail:      fmt.Sprintf("Failed to delete: %v", err),
			})
		} else {
			steps = append(steps, StepResult{
				Description: "DDEV project",
				Detail:      "Deleted",
			})
		}
	} else {
		steps = append(steps, StepResult{
			Description: "DDEV project",
			Detail:      "Skipped (no .ddev/config.yaml)",
		})
	}

	// Step 2: Remove git worktree (run from the project root)
	fmt.Println("\n--- Removing git worktree ---")
	wtCmd := exec.Command("git", "worktree", "remove", "--force", targetPath)
	wtCmd.Dir = projectRoot
	wtCmd.Stdout = os.Stdout
	wtCmd.Stderr = os.Stderr
	if err := wtCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing worktree: %v\n", err)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Git worktree",
		Detail:      "Removed " + targetPath,
	})

	// Step 3: Delete the branch
	fmt.Println("\n--- Deleting branch ---")
	branchCmd := exec.Command("git", "branch", "-D", branchName)
	branchCmd.Dir = projectRoot
	branchCmd.Stdout = os.Stdout
	branchCmd.Stderr = os.Stderr
	if err := branchCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to delete branch %s: %v\n", branchName, err)
		steps = append(steps, StepResult{
			Description: "Branch",
			Detail:      fmt.Sprintf("Failed to delete %s: %v", branchName, err),
		})
	} else {
		steps = append(steps, StepResult{
			Description: "Branch",
			Detail:      "Deleted " + branchName,
		})
	}

	// Step 4: Prune Docker build cache
	fmt.Println("\n--- Pruning Docker build cache ---")
	pruneCmd := exec.Command("docker", "builder", "prune", "-f")
	pruneCmd.Stdout = os.Stdout
	pruneCmd.Stderr = os.Stderr
	if err := pruneCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to prune Docker build cache: %v\n", err)
		steps = append(steps, StepResult{
			Description: "Docker build cache",
			Detail:      fmt.Sprintf("Failed to prune: %v", err),
		})
	} else {
		steps = append(steps, StepResult{
			Description: "Docker build cache",
			Detail:      "Pruned",
		})
	}

	// Summary
	fmt.Println()
	fmt.Println("=== Workspace Removal Complete ===")
	fmt.Println()
	for _, step := range steps {
		fmt.Printf("  %-25s %s\n", step.Description+":", step.Detail)
	}
	fmt.Println()
}

// validateWorktree checks that targetPath is a git worktree and returns its
// branch name. It runs git commands from projectRoot and skips bare repo entries.
func validateWorktree(targetPath, projectRoot string) (branch string, err error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}

	var currentWorktree string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
		}
		// Skip bare repo entries
		if line == "bare" {
			currentWorktree = ""
			continue
		}
		if strings.HasPrefix(line, "branch ") && currentWorktree == targetPath {
			ref := strings.TrimPrefix(line, "branch ")
			// Strip "refs/heads/" prefix to get the short branch name
			branch = strings.TrimPrefix(ref, "refs/heads/")
			return branch, nil
		}
	}

	return "", fmt.Errorf("%s is not a git worktree", targetPath)
}

func getDDEVProjectName(dir string) (string, error) {
	configPath := filepath.Join(dir, ".ddev", "config.yaml")
	f, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("could not open %s: %w", configPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "name: ") {
			return strings.TrimPrefix(line, "name: "), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading %s: %w", configPath, err)
	}

	return "", fmt.Errorf("no 'name:' field found in %s", configPath)
}

func createWorktree(projectRoot, name, baseBranch string) error {
	// Ensure spaces/ directory exists
	spacesDir := filepath.Join(projectRoot, "spaces")
	if err := os.MkdirAll(spacesDir, 0755); err != nil {
		return fmt.Errorf("could not create spaces directory: %w", err)
	}

	// Check if the branch already exists
	checkCmd := exec.Command("git", "rev-parse", "--verify", name)
	checkCmd.Dir = projectRoot
	branchExists := checkCmd.Run() == nil

	var gitArgs []string
	if branchExists {
		// Branch exists — check it out directly
		gitArgs = []string{"worktree", "add", filepath.Join("spaces", name), name}
	} else {
		// Branch doesn't exist — create it
		gitArgs = []string{"worktree", "add", "-b", name, filepath.Join("spaces", name)}
		if baseBranch != "" {
			gitArgs = append(gitArgs, baseBranch)
		}
	}
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func renameDDEVProject(worktreePath, identifier, originalName string) error {
	configPath := filepath.Join(worktreePath, ".ddev", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", configPath, err)
	}

	oldLine := "name: " + originalName
	newLine := "name: " + identifier + "-" + originalName
	content := string(data)

	if !strings.Contains(content, oldLine) {
		return fmt.Errorf("could not find '%s' in %s", oldLine, configPath)
	}

	content = strings.Replace(content, oldLine, newLine, 1)

	err = os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", configPath, err)
	}

	return nil
}

func updateSettingsDdevPHP(worktreePath, ddevName string) error {
	settingsPath := filepath.Join(worktreePath, "web", "sites", "default", "settings.ddev.php")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", settingsPath, err)
	}

	content := string(data)

	// Remove the first comment block (/* ... */)
	commentRe := regexp.MustCompile(`(?s)/\*.*?\*/\s*`)
	loc := commentRe.FindStringIndex(content)
	if loc != nil {
		content = content[:loc[0]] + content[loc[1]:]
	}

	// Set $host to the new DDEV server name
	hostRe := regexp.MustCompile(`\$host\s*=\s*["'].*?["']`)
	newHost := `$host = "ddev-` + ddevName + `-db"`
	if !hostRe.MatchString(content) {
		return fmt.Errorf("could not find $host assignment in %s", settingsPath)
	}
	content = hostRe.ReplaceAllString(content, newHost)

	err = os.WriteFile(settingsPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", settingsPath, err)
	}

	return nil
}

func runCommandLive(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func handleDBImport(worktreePath, projectRoot string) (string, error) {
	defaultPath := filepath.Join(projectRoot, "db", "db.sql.gz")

	if _, err := os.Stat(defaultPath); err == nil {
		fmt.Printf("\nFound database dump at %s\n", defaultPath)
		fmt.Println("--- Importing database ---")
		err := runCommandLive(worktreePath, "ddev", "import-db", "--file="+defaultPath)
		if err != nil {
			return "", err
		}
		return "Imported from " + defaultPath, nil
	}

	// Prompt user
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nNo database dump found at db/db.sql.gz\n")
	fmt.Print("Enter path to database dump (or press Enter to skip): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("error reading input: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" {
		return "Skipped (no import)", nil
	}

	// Resolve the path
	if !filepath.IsAbs(input) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("error getting working directory: %w", err)
		}
		input = filepath.Join(cwd, input)
	}

	if _, err := os.Stat(input); err != nil {
		return "", fmt.Errorf("file not found: %s", input)
	}

	fmt.Println("--- Importing database ---")
	err = runCommandLive(worktreePath, "ddev", "import-db", "--file="+input)
	if err != nil {
		return "", err
	}
	return "Imported from " + input, nil
}

func printSummary(steps []StepResult) {
	fmt.Println("=== Workspace Setup Complete ===")
	fmt.Println()
	for _, step := range steps {
		fmt.Printf("  %-25s %s\n", step.Description+":", step.Detail)
	}
	fmt.Println()
}

func cleanup(state *cleanupState) {
	fmt.Fprintf(os.Stderr, "\n--- Cleaning up ---\n")

	if state.ddevStarted {
		fmt.Fprintf(os.Stderr, "Deleting DDEV project...\n")
		cmd := exec.Command("ddev", "delete", "-O", "--omit-snapshot")
		cmd.Dir = state.worktreePath
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to delete DDEV project: %v\n", err)
		}
	}

	if state.worktreeCreated {
		fmt.Fprintf(os.Stderr, "Removing git worktree...\n")
		cmd := exec.Command("git", "worktree", "remove", "--force", state.worktreePath)
		cmd.Dir = state.projectRoot
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree: %v\n", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Cleanup complete.\n")
}
