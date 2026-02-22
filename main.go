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
  worktreePath   string
  worktreeCreated bool
  ddevStarted    bool
}

func main() {
  worktreeName, identifier, err := parseArgs()
  if err != nil {
    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
    fmt.Fprintf(os.Stderr, "Usage: workspace <worktree-name> [identifier]\n")
    os.Exit(1)
  }

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

func parseArgs() (worktreeName, identifier string, err error) {
  args := os.Args[1:]
  if len(args) < 1 || len(args) > 2 {
    return "", "", fmt.Errorf("expected 1 or 2 arguments, got %d", len(args))
  }

  worktreeName = args[0]
  if worktreeName == "" {
    return "", "", fmt.Errorf("worktree name cannot be empty")
  }

  if len(args) == 2 {
    identifier = args[1]
  } else {
    if len(worktreeName) < 4 {
      identifier = worktreeName
    } else {
      identifier = worktreeName[:4]
    }
  }

  return worktreeName, identifier, nil
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
