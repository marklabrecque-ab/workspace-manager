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
	ddevName        string
}

type ProjectType string

const (
	ProjectDrupal    ProjectType = "drupal"
	ProjectWordPress ProjectType = "wordpress"
	ProjectUnsupported ProjectType = "unsupported"
)

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
	case "refresh":
		cmdRefresh(args[1:])
	case "list", "ls":
		cmdList()
	case "projects":
		cmdProjects()
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
  projects                 List all workspace projects in ~/Projects

Examples:
  workspace init git@github.com:user/project.git
  workspace init git@github.com:user/project.git myproject
  workspace new 0001-new-task
  workspace new 0001-new-task t1              (custom DDEV identifier)
  workspace new --base develop 0001-new-task  (branch off develop)
  workspace remove 0001-new-task     (remove by name)
  workspace remove                   (remove current directory's worktree)
  workspace list                     (list all workspaces)
  workspace refresh [name]           (drop and reimport the database)
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

// deriveIdentifier generates a short identifier from a worktree name.
// It takes the first 4 characters, but if that ends with a hyphen, it
// uses a "0" prefix plus the first 3 characters instead
// (e.g. "123-foo" → "0123" not "123-").
func deriveIdentifier(worktreeName string) string {
	var id string
	if len(worktreeName) < 4 {
		id = worktreeName
	} else {
		id = worktreeName[:4]
	}
	if strings.HasSuffix(id, "-") {
		id = "0" + worktreeName[:3]
	}
	return id
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
		fmt.Fprintf(os.Stderr, "Error: could not detect default branch.\n")
		fmt.Fprintf(os.Stderr, "Neither 'develop' nor 'main' branches were found on the remote.\n")
		fmt.Fprintf(os.Stderr, "Please ensure the remote repository has a 'develop' or 'main' branch.\n")
		cleanupInit(projectDir)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Default branch",
		Detail:      defaultBranch,
	})

	// Step 6: Create spaces/, db/, and files/ directories, then first worktree
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
	filesDir := filepath.Join(projectDir, "files")
	if err := os.MkdirAll(filesDir, 0777); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating files directory: %v\n", err)
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

	// Detect project type from DDEV config
	projectType := getDDEVProjectType(worktreeFullPath)
	steps = append(steps, StepResult{
		Description: "Project type",
		Detail:      string(projectType),
	})

	// Link project files
	filesDetail, err := linkProjectFiles(worktreeFullPath, projectDir, projectType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nWarning: failed to link project files: %v\n", err)
		steps = append(steps, StepResult{
			Description: "Project files",
			Detail:      "Failed: " + err.Error(),
		})
	} else {
		steps = append(steps, StepResult{
			Description: "Project files",
			Detail:      filesDetail,
		})
	}

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

			if projectType == ProjectDrupal {
				fmt.Println("\n--- Running composer install ---")
				if err := runCommandLive(worktreeFullPath, "ddev", "composer", "install"); err != nil {
					fmt.Fprintf(os.Stderr, "\nWarning: failed to run composer install: %v\n", err)
					steps = append(steps, StepResult{
						Description: "Composer install",
						Detail:      fmt.Sprintf("Failed: %v", err),
					})
				} else {
					steps = append(steps, StepResult{
						Description: "Composer install",
						Detail:      "Complete",
					})
				}
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
	// Prefer develop, fall back to main
	for _, branch := range []string{"develop", "main"} {
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

type worktreeEntry struct {
	path   string
	branch string
	isBare bool
}

// parseWorktreeList parses the output of `git worktree list --porcelain`
// into a slice of worktreeEntry structs.
func parseWorktreeList(output string) []worktreeEntry {
	var entries []worktreeEntry
	var current worktreeEntry
	inEntry := false

	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if inEntry {
				entries = append(entries, current)
			}
			current = worktreeEntry{path: strings.TrimPrefix(line, "worktree ")}
			inEntry = true
		} else if line == "bare" {
			current.isBare = true
		} else if strings.HasPrefix(line, "branch ") {
			ref := strings.TrimPrefix(line, "branch ")
			current.branch = strings.TrimPrefix(ref, "refs/heads/")
		} else if line == "" {
			if inEntry {
				entries = append(entries, current)
				current = worktreeEntry{}
				inEntry = false
			}
		}
	}
	// Handle last entry if output doesn't end with a blank line
	if inEntry {
		entries = append(entries, current)
	}

	return entries
}

func cmdProjects() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine home directory: %v\n", err)
		os.Exit(1)
	}

	projectsDir := filepath.Join(homeDir, "Projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No ~/Projects directory found.")
			return
		}
		fmt.Fprintf(os.Stderr, "Error reading ~/Projects: %v\n", err)
		os.Exit(1)
	}

	type worktreeInfo struct {
		name   string
		branch string
	}

	type projectInfo struct {
		name       string
		path       string
		worktrees  []worktreeInfo
	}

	var projects []projectInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(projectsDir, entry.Name())
		spacesDir := filepath.Join(dirPath, "spaces")

		if info, err := os.Stat(spacesDir); err != nil || !info.IsDir() {
			continue
		}

		// Run git worktree list --porcelain
		cmd := exec.Command("git", "worktree", "list", "--porcelain")
		cmd.Dir = dirPath
		out, err := cmd.Output()
		if err != nil {
			continue
		}

		var worktrees []worktreeInfo
		for _, entry := range parseWorktreeList(string(out)) {
			if !entry.isBare && strings.HasPrefix(entry.path, spacesDir+string(filepath.Separator)) {
				name := strings.TrimPrefix(entry.path, spacesDir+string(filepath.Separator))
				worktrees = append(worktrees, worktreeInfo{name: name, branch: entry.branch})
			}
		}

		projects = append(projects, projectInfo{
			name:      entry.Name(),
			path:      dirPath,
			worktrees: worktrees,
		})
	}

	if len(projects) == 0 {
		fmt.Println("No workspace projects found in ~/Projects.")
		return
	}

	for i, proj := range projects {
		fmt.Printf("%s (~/Projects/%s)\n", proj.name, proj.name)

		if len(proj.worktrees) == 0 {
			fmt.Println("  (no worktrees)")
		} else {
			maxName := 0
			for _, wt := range proj.worktrees {
				if len(wt.name) > maxName {
					maxName = len(wt.name)
				}
			}
			for _, wt := range proj.worktrees {
				if wt.branch != "" {
					fmt.Printf("  %-*s  (%s)\n", maxName, wt.name, wt.branch)
				} else {
					fmt.Printf("  %-*s  (detached)\n", maxName, wt.name)
				}
			}
		}

		if i < len(projects)-1 {
			fmt.Println()
		}
	}
}

func cmdList() {
	projectRoot, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Detect project type from DDEV config
	projectType := detectProjectType(projectRoot)
	_ = projectType

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
	for _, entry := range parseWorktreeList(string(out)) {
		if !entry.isBare && strings.HasPrefix(entry.path, spacesDir+string(filepath.Separator)) {
			name := strings.TrimPrefix(entry.path, spacesDir+string(filepath.Separator))
			workspaces = append(workspaces, workspace{
				name:   name,
				branch: entry.branch,
				path:   entry.path,
			})
		}
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

type newArgs struct {
	worktreeName       string
	identifier         string
	baseBranch         string
	identifierExplicit bool
}

// parseNewArgs parses the arguments for the "new" subcommand.
func parseNewArgs(args []string) (newArgs, error) {
	var baseBranch string
	var positional []string

	for i := 0; i < len(args); i++ {
		if args[i] == "--base" {
			if i+1 >= len(args) {
				return newArgs{}, fmt.Errorf("--base requires a branch name")
			}
			baseBranch = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "--base=") {
			baseBranch = strings.TrimPrefix(args[i], "--base=")
		} else {
			positional = append(positional, args[i])
		}
	}

	if len(positional) < 1 || len(positional) > 2 {
		return newArgs{}, fmt.Errorf("expected 1 or 2 positional arguments, got %d", len(positional))
	}

	worktreeName := positional[0]
	if worktreeName == "" {
		return newArgs{}, fmt.Errorf("worktree name cannot be empty")
	}

	var identifier string
	identifierExplicit := len(positional) == 2
	if identifierExplicit {
		identifier = positional[1]
	} else {
		identifier = deriveIdentifier(worktreeName)
	}

	return newArgs{
		worktreeName:       worktreeName,
		identifier:         identifier,
		baseBranch:         baseBranch,
		identifierExplicit: identifierExplicit,
	}, nil
}

func cmdNewFromArgs(args []string) {
	parsed, err := parseNewArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Usage: workspace new [--base <branch>] <worktree-name> [identifier]\n")
		os.Exit(1)
	}
	cmdNew(parsed.worktreeName, parsed.identifier, parsed.baseBranch, parsed.identifierExplicit)
}

func cmdNew(worktreeName, identifier, baseBranch string, identifierExplicit bool) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Fetch latest refs from origin
	fmt.Println("--- Fetching latest changes ---")
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = projectRoot
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch from origin: %v\n", err)
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

	// Step 1: Create git worktree
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

	// Step 2: Push branch and set up tracking if it doesn't exist on the remote
	remoteBranchCheck := exec.Command("git", "rev-parse", "--verify", "refs/remotes/origin/"+worktreeName)
	remoteBranchCheck.Dir = projectRoot
	if remoteBranchCheck.Run() != nil {
		fmt.Println("\n--- Pushing branch to remote ---")
		pushCmd := exec.Command("git", "push", "-u", "origin", worktreeName)
		pushCmd.Dir = worktreePath
		pushCmd.Stdout = os.Stdout
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to push branch to remote: %v\n", err)
			steps = append(steps, StepResult{
				Description: "Push branch to remote",
				Detail:      "Failed (can be pushed manually later)",
			})
		} else {
			steps = append(steps, StepResult{
				Description: "Pushed branch to remote",
				Detail:      worktreeName + " → origin/" + worktreeName,
			})
		}
	} else {
		steps = append(steps, StepResult{
			Description: "Remote branch",
			Detail:      "Already exists, skipped push",
		})
	}

	// Step 3: Detect DDEV from the new worktree
	originalName, err := getDDEVProjectName(worktreePath)
	hasDDEV := err == nil
	projectType := getDDEVProjectType(worktreePath)

	if !hasDDEV {
		steps = append(steps, StepResult{
			Description: "DDEV",
			Detail:      "Skipped (no .ddev/config.yaml found)",
		})
		fmt.Println()
		printSummary(steps)
		return
	}

	steps = append(steps, StepResult{
		Description: "Read DDEV project name",
		Detail:      originalName,
	})

	// Step 4: Rename DDEV project (skip for develop/main — keep default name,
	// unless the user explicitly provided an identifier to override it)
	isDefaultBranch := (worktreeName == "develop" || worktreeName == "main") && !identifierExplicit
	ddevName := originalName
	if !isDefaultBranch {
		ddevName = identifier + "-" + originalName
		err = createDDEVLocalConfig(worktreePath, ddevName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating DDEV local config: %v\n", err)
			cleanup(state)
			os.Exit(1)
		}
		steps = append(steps, StepResult{
			Description: "Created DDEV local config",
			Detail:      ddevName,
		})

		// Update settings.ddev.php with new DB host (Drupal projects)
		if projectType == ProjectDrupal {
			settingsPath := filepath.Join(worktreePath, "web", "sites", "default", "settings.ddev.php")
			if _, statErr := os.Stat(settingsPath); statErr == nil {
				err = updateSettingsDdevPHP(settingsPath, ddevName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error updating settings.ddev.php: %v\n", err)
					cleanup(state)
					os.Exit(1)
				}
				assumeCmd := exec.Command("git", "update-index", "--assume-unchanged", filepath.Join("web", "sites", "default", "settings.ddev.php"))
				assumeCmd.Dir = worktreePath
				_ = assumeCmd.Run()
				steps = append(steps, StepResult{
					Description: "Updated settings.ddev.php",
					Detail:      "DB host set to ddev-" + ddevName + "-db",
				})
			}
		}
	} else {
		steps = append(steps, StepResult{
			Description: "DDEV project name",
			Detail:      originalName + " (kept default)",
		})
	}

	// Remove .ddev/traefik so DDEV regenerates it for the new project
	traefikPath := filepath.Join(worktreePath, ".ddev", "traefik")
	if err := os.RemoveAll(traefikPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing .ddev/traefik: %v\n", err)
		cleanup(state)
		os.Exit(1)
	}

	// Link project files (before DDEV start so files are available immediately)
	filesDetail, err := linkProjectFiles(worktreePath, projectRoot, projectType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nWarning: failed to link project files: %v\n", err)
		steps = append(steps, StepResult{
			Description: "Project files",
			Detail:      "Failed: " + err.Error(),
		})
	} else {
		steps = append(steps, StepResult{
			Description: "Project files",
			Detail:      filesDetail,
		})
	}

	// Step 4: Start DDEV
	state.ddevName = ddevName
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

	// Step 5: Composer install for Drupal projects
	if projectType == ProjectDrupal {
		fmt.Println("\n--- Running composer install ---")
		if err := runCommandLive(worktreePath, "ddev", "composer", "install"); err != nil {
			fmt.Fprintf(os.Stderr, "\nWarning: failed to run composer install: %v\n", err)
			steps = append(steps, StepResult{
				Description: "Composer install",
				Detail:      fmt.Sprintf("Failed: %v", err),
			})
		} else {
			steps = append(steps, StepResult{
				Description: "Composer install",
				Detail:      "Complete",
			})
		}
	}

	// Step 6: Handle DB import
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

func cmdRefresh(args []string) {
  projectRoot, err := findProjectRoot()
  if err != nil {
    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
    os.Exit(1)
  }

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

  if _, err := validateWorktree(targetPath, projectRoot); err != nil {
    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
    os.Exit(1)
  }

  var steps []StepResult

  dbDetail, err := handleDBImport(targetPath, projectRoot)
  if err != nil {
    fmt.Fprintf(os.Stderr, "\nError importing database: %v\n", err)
    os.Exit(1)
  }
  steps = append(steps, StepResult{
    Description: "Database",
    Detail:      dbDetail,
  })

  fmt.Println()
  printSummary(steps)
}

// findDDEVProjectName reads the DDEV project name from the main/master
// worktree, which always has the original (un-prefixed) name.
// detectProjectType reads the DDEV project type from the first existing worktree.
func detectProjectType(projectRoot string) ProjectType {
	spacesDir := filepath.Join(projectRoot, "spaces")

	entries, err := os.ReadDir(spacesDir)
	if err != nil {
		return ProjectUnsupported
	}

	for _, entry := range entries {
		if entry.IsDir() {
			return getDDEVProjectType(filepath.Join(spacesDir, entry.Name()))
		}
	}

	return ProjectUnsupported
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

	// Detect project type from DDEV config
	projectType := getDDEVProjectType(targetPath)
	_ = projectType

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
	ddevName, ddevErr := getDDEVProjectName(targetPath)
	if ddevErr == nil {
		fmt.Println("\n--- Deleting DDEV project ---")
		// Pass the project name explicitly so DDEV can clean up its
		// global registration even if the directory disappears later.
		ddevCmd := exec.Command("ddev", "delete", "--omit-snapshot", "-y", ddevName)
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
				Detail:      "Deleted (" + ddevName + ")",
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

	for _, entry := range parseWorktreeList(string(out)) {
		if !entry.isBare && entry.path == targetPath {
			return entry.branch, nil
		}
	}

	return "", fmt.Errorf("%s is not a git worktree", targetPath)
}

func getDDEVProjectName(dir string) (string, error) {
	// Check config.local.yaml first for a name override
	localConfigPath := filepath.Join(dir, ".ddev", "config.local.yaml")
	if name, err := readDDEVName(localConfigPath); err == nil {
		return name, nil
	}

	// Fall back to config.yaml
	configPath := filepath.Join(dir, ".ddev", "config.yaml")
	if name, err := readDDEVName(configPath); err == nil {
		return name, nil
	}

	return "", fmt.Errorf("no 'name:' field found in %s or %s", localConfigPath, configPath)
}

func readDDEVName(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
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
		return "", err
	}

	return "", fmt.Errorf("no 'name:' field found in %s", path)
}

func getDDEVProjectType(dir string) ProjectType {
	configPath := filepath.Join(dir, ".ddev", "config.yaml")
	f, err := os.Open(configPath)
	if err != nil {
		return ProjectUnsupported
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "type: ") {
			value := strings.TrimPrefix(line, "type: ")
			if strings.HasPrefix(value, "drupal") {
				return ProjectDrupal
			}
			if strings.HasPrefix(value, "wordpress") {
				return ProjectWordPress
			}
			return ProjectUnsupported
		}
	}

	return ProjectUnsupported
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
			gitArgs = append(gitArgs, "--no-track", baseBranch)
		}
	}
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createDDEVLocalConfig(worktreePath, ddevName string) error {
	localConfigPath := filepath.Join(worktreePath, ".ddev", "config.local.yaml")
	content := "name: " + ddevName + "\n"
	err := os.WriteFile(localConfigPath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", localConfigPath, err)
	}
	return nil
}

func updateSettingsDdevPHP(settingsPath, ddevName string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", settingsPath, err)
	}

	content := string(data)

	// Set $host to the new DDEV container name
	hostRe := regexp.MustCompile(`\$host\s*=\s*["'].*?["']`)
	newHost := `$host = "ddev-` + ddevName + `-db"`
	if !hostRe.MatchString(content) {
		return fmt.Errorf("could not find $host assignment in %s", settingsPath)
	}
	content = hostRe.ReplaceAllLiteralString(content, newHost)

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

func linkProjectFiles(worktreePath, projectRoot string, projectType ProjectType) (string, error) {
	var dest string
	switch projectType {
	case ProjectDrupal:
		dest = filepath.Join(worktreePath, "web", "sites", "default", "files")
	case ProjectWordPress:
		dest = filepath.Join(worktreePath, "web", "wp-content", "uploads")
	default:
		return "Skipped (unsupported project type)", nil
	}

	source := filepath.Join(projectRoot, "files")

	// Ensure the shared files directory exists
	if err := os.MkdirAll(source, 0777); err != nil {
		return "", fmt.Errorf("could not create files directory: %w", err)
	}

	// Check if dest is already the correct symlink
	if target, err := os.Readlink(dest); err == nil {
		absTarget := target
		if !filepath.IsAbs(absTarget) {
			absTarget = filepath.Join(filepath.Dir(dest), target)
		}
		absTarget, _ = filepath.Abs(absTarget)
		absSource, _ := filepath.Abs(source)
		if absTarget == absSource {
			return fmt.Sprintf("Symlink already exists: %s", dest), nil
		}
	}

	// If dest exists as a real directory, remove it
	if info, err := os.Lstat(dest); err == nil {
		if info.IsDir() {
			if err := os.RemoveAll(dest); err != nil {
				return "", fmt.Errorf("could not remove existing directory %s: %w", dest, err)
			}
		} else {
			if err := os.Remove(dest); err != nil {
				return "", fmt.Errorf("could not remove existing file %s: %w", dest, err)
			}
		}
	}

	// Ensure the parent directory of dest exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", fmt.Errorf("could not create parent directory: %w", err)
	}

	// Compute relative path from dest's parent to source
	relPath, err := filepath.Rel(filepath.Dir(dest), source)
	if err != nil {
		return "", fmt.Errorf("could not compute relative path: %w", err)
	}

	if err := os.Symlink(relPath, dest); err != nil {
		return "", fmt.Errorf("could not create symlink: %w", err)
	}

	return fmt.Sprintf("Linked %s → %s", dest, relPath), nil
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

	if state.ddevStarted && state.ddevName != "" {
		fmt.Fprintf(os.Stderr, "Deleting DDEV project...\n")
		cmd := exec.Command("ddev", "delete", "-O", "-y", state.ddevName)
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
		// Ensure the directory is removed even if worktree removal failed
		if _, err := os.Stat(state.worktreePath); err == nil {
			fmt.Fprintf(os.Stderr, "Removing leftover directory...\n")
			if err := os.RemoveAll(state.worktreePath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove directory: %v\n", err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Cleanup complete.\n")
}
