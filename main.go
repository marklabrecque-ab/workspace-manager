package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type StepResult struct {
	Description string
	Detail      string
}

type cleanupState struct {
	worktreePath    string
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
	case "remove":
		cmdRemove(args[1:])
	case "new":
		cmdNewFromArgs(args[1:])
	case "--help", "-h":
		printUsage()
		os.Exit(0)
	default:
		// Implicit "new": treat all args as name [identifier]
		cmdNewFromArgs(args)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: workspace <command> [arguments]

Commands:
  new <name> [identifier]   Create a new worktree + DDEV environment (default)
  remove [name]             Remove a worktree + DDEV environment

If no command is specified, "new" is assumed.

Examples:
  workspace new 0001-new-task
  workspace 0001-new-task            (same as above)
  workspace new 0001-new-task t1     (custom DDEV identifier)
  workspace remove 0001-new-task     (remove by name)
  workspace remove                   (remove current directory's worktree)
`)
}

func cmdNewFromArgs(args []string) {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Error: expected 1 or 2 arguments, got %d\n", len(args))
		fmt.Fprintf(os.Stderr, "Usage: workspace [new] <worktree-name> [identifier]\n")
		os.Exit(1)
	}

	worktreeName := args[0]
	if worktreeName == "" {
		fmt.Fprintf(os.Stderr, "Error: worktree name cannot be empty\n")
		os.Exit(1)
	}

	var identifier string
	if len(args) == 2 {
		identifier = args[1]
	} else {
		if len(worktreeName) < 4 {
			identifier = worktreeName
		} else {
			identifier = worktreeName[:4]
		}
	}

	cmdNew(worktreeName, identifier)
}

func cmdNew(worktreeName, identifier string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	worktreePath := filepath.Join(cwd, "..", worktreeName)
	state := &cleanupState{worktreePath: worktreePath}
	var steps []StepResult

	// Step 1: Read current DDEV project name
	originalName, err := getDDEVProjectName(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure you're in a DDEV project directory.\n")
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Read DDEV project name",
		Detail:      originalName,
	})

	// Step 2: Create git worktree
	err = createWorktree(worktreeName)
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

	// Step 3: Rename DDEV project
	newName := identifier + "-" + originalName
	err = renameDDEVProject(worktreePath, identifier, originalName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error renaming DDEV project: %v\n", err)
		cleanup(state)
		os.Exit(1)
	}
	steps = append(steps, StepResult{
		Description: "Renamed DDEV project",
		Detail:      newName,
	})

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
		Detail:      newName,
	})

	// Step 5: Handle DB import
	dbDetail, err := handleDBImport(worktreePath)
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

func cmdRemove(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	// Determine target directory
	var targetPath string
	if len(args) > 0 && args[0] != "" {
		targetPath = filepath.Join(cwd, "..", args[0])
	} else {
		targetPath = cwd
	}

	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Validate it's a git worktree
	branchName, err := validateWorktree(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Confirmation prompt
	fmt.Println("The following will be destroyed:")
	fmt.Printf("  Worktree:  %s\n", targetPath)
	fmt.Printf("  Branch:    %s\n", branchName)
	fmt.Printf("  DDEV project in that worktree\n")
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

	// Step 1: Delete DDEV
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

	// Step 2: Remove git worktree (run from outside the worktree)
	fmt.Println("\n--- Removing git worktree ---")
	mainRepo := filepath.Dir(targetPath)
	wtCmd := exec.Command("git", "worktree", "remove", "--force", targetPath)
	wtCmd.Dir = mainRepo
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
	branchCmd.Dir = mainRepo
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

	// Summary
	fmt.Println()
	fmt.Println("=== Workspace Removal Complete ===")
	fmt.Println()
	for _, step := range steps {
		fmt.Printf("  %-25s %s\n", step.Description+":", step.Detail)
	}
	fmt.Println()
}

// validateWorktree checks that targetPath is a git worktree and returns its branch name.
func validateWorktree(targetPath string) (string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}

	var currentWorktree string
	var branch string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
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

func createWorktree(name string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", name, filepath.Join("..", name))
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

func runCommandLive(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func handleDBImport(worktreePath string) (string, error) {
	defaultPath := filepath.Join(worktreePath, "tmp", "db.sql.gz")

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
	fmt.Print("\nNo database dump found at tmp/db.sql.gz\n")
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
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree: %v\n", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Cleanup complete.\n")
}
