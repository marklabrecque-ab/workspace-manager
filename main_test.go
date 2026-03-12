package main

import (
  "bytes"
  "io"
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestDeriveIdentifier(t *testing.T) {
  tests := []struct {
    name     string
    input    string
    expected string
  }{
    {"standard 4+ chars", "0001-new-task", "0001"},
    {"trailing hyphen gets zero prefix", "123-some-ticket", "0123"},
    {"short name under 4 chars", "abc", "abc"},
    {"exactly 4 chars no hyphen", "abcd", "abcd"},
    {"exactly 4 chars with trailing hyphen", "abc-", "0abc"},
    {"long name no early hyphen", "feat-something", "feat"},
    {"single char", "x", "x"},
    {"hyphen at position 1", "a-bc-thing", "a-bc"},
    {"all hyphens", "----rest", "0---"},
    {"numeric no hyphen", "1234-thing", "1234"},
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got := deriveIdentifier(tt.input)
      if got != tt.expected {
        t.Errorf("deriveIdentifier(%q) = %q, want %q", tt.input, got, tt.expected)
      }
    })
  }
}

func TestExtractProjectName(t *testing.T) {
  tests := []struct {
    name     string
    input    string
    expected string
  }{
    {"SSH URL with .git", "git@github.com:user/project.git", "project"},
    {"SSH URL without .git", "git@github.com:user/project", "project"},
    {"HTTPS URL with .git", "https://github.com/user/project.git", "project"},
    {"HTTPS URL without .git", "https://github.com/user/project", "project"},
    {"trailing slash stripped", "https://github.com/user/project/", "project"},
    {"trailing slash with .git", "https://github.com/user/project.git/", "project"},
    {"deep path", "https://gitlab.com/org/sub/repo.git", "repo"},
    {"bare name", "myrepo.git", "myrepo"},
    {"bare name no suffix", "myrepo", "myrepo"},
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got := extractProjectName(tt.input)
      if got != tt.expected {
        t.Errorf("extractProjectName(%q) = %q, want %q", tt.input, got, tt.expected)
      }
    })
  }
}

func TestParseWorktreeList(t *testing.T) {
  tests := []struct {
    name     string
    input    string
    expected []worktreeEntry
  }{
    {
      name:     "empty input",
      input:    "",
      expected: nil,
    },
    {
      name: "single worktree with branch",
      input: "worktree /home/user/project/spaces/main\nHEAD abc1234\nbranch refs/heads/main\n\n",
      expected: []worktreeEntry{
        {path: "/home/user/project/spaces/main", branch: "main"},
      },
    },
    {
      name: "bare entry is flagged",
      input: "worktree /home/user/project\nbare\n\n",
      expected: []worktreeEntry{
        {path: "/home/user/project", isBare: true},
      },
    },
    {
      name: "multiple worktrees including bare",
      input: "worktree /home/user/project\nbare\n\nworktree /home/user/project/spaces/main\nHEAD abc1234\nbranch refs/heads/main\n\nworktree /home/user/project/spaces/feature\nHEAD def5678\nbranch refs/heads/feature-branch\n\n",
      expected: []worktreeEntry{
        {path: "/home/user/project", isBare: true},
        {path: "/home/user/project/spaces/main", branch: "main"},
        {path: "/home/user/project/spaces/feature", branch: "feature-branch"},
      },
    },
    {
      name: "detached HEAD (no branch line)",
      input: "worktree /home/user/project/spaces/detached\nHEAD abc1234\ndetached\n\n",
      expected: []worktreeEntry{
        {path: "/home/user/project/spaces/detached"},
      },
    },
    {
      name: "no trailing newline",
      input: "worktree /home/user/project/spaces/main\nHEAD abc1234\nbranch refs/heads/main",
      expected: []worktreeEntry{
        {path: "/home/user/project/spaces/main", branch: "main"},
      },
    },
    {
      name: "multiple entries no trailing newline",
      input: "worktree /path/a\nbare\n\nworktree /path/b\nbranch refs/heads/dev",
      expected: []worktreeEntry{
        {path: "/path/a", isBare: true},
        {path: "/path/b", branch: "dev"},
      },
    },
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got := parseWorktreeList(tt.input)
      if len(got) != len(tt.expected) {
        t.Fatalf("parseWorktreeList() returned %d entries, want %d", len(got), len(tt.expected))
      }
      for i, entry := range got {
        exp := tt.expected[i]
        if entry.path != exp.path {
          t.Errorf("entry[%d].path = %q, want %q", i, entry.path, exp.path)
        }
        if entry.branch != exp.branch {
          t.Errorf("entry[%d].branch = %q, want %q", i, entry.branch, exp.branch)
        }
        if entry.isBare != exp.isBare {
          t.Errorf("entry[%d].isBare = %v, want %v", i, entry.isBare, exp.isBare)
        }
      }
    })
  }
}

func TestParseNewArgs(t *testing.T) {
  tests := []struct {
    name      string
    args      []string
    expected  newArgs
    expectErr string
  }{
    {
      name: "simple worktree name",
      args: []string{"0001-new-task"},
      expected: newArgs{
        worktreeName: "0001-new-task",
        identifier:   "0001",
      },
    },
    {
      name: "worktree name with explicit identifier",
      args: []string{"0001-new-task", "t1"},
      expected: newArgs{
        worktreeName:       "0001-new-task",
        identifier:         "t1",
        identifierExplicit: true,
      },
    },
    {
      name: "with --base flag",
      args: []string{"--base", "develop", "0001-new-task"},
      expected: newArgs{
        worktreeName: "0001-new-task",
        identifier:   "0001",
        baseBranch:   "develop",
      },
    },
    {
      name: "with --base= form",
      args: []string{"--base=develop", "0001-new-task"},
      expected: newArgs{
        worktreeName: "0001-new-task",
        identifier:   "0001",
        baseBranch:   "develop",
      },
    },
    {
      name: "all options combined",
      args: []string{"--base", "main", "0001-new-task", "custom"},
      expected: newArgs{
        worktreeName:       "0001-new-task",
        identifier:         "custom",
        baseBranch:         "main",
        identifierExplicit: true,
      },
    },
    {
      name: "--base at end of args",
      args: []string{"0001-new-task", "--base", "develop"},
      expected: newArgs{
        worktreeName: "0001-new-task",
        identifier:   "0001",
        baseBranch:   "develop",
      },
    },
    {
      name:      "no arguments",
      args:      []string{},
      expectErr: "expected 1 or 2 positional arguments, got 0",
    },
    {
      name:      "too many positional arguments",
      args:      []string{"a", "b", "c"},
      expectErr: "expected 1 or 2 positional arguments, got 3",
    },
    {
      name:      "--base without value",
      args:      []string{"--base"},
      expectErr: "--base requires a branch name",
    },
    {
      name:      "empty worktree name",
      args:      []string{""},
      expectErr: "worktree name cannot be empty",
    },
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      got, err := parseNewArgs(tt.args)
      if tt.expectErr != "" {
        if err == nil {
          t.Fatalf("expected error %q, got nil", tt.expectErr)
        }
        if !strings.Contains(err.Error(), tt.expectErr) {
          t.Fatalf("expected error containing %q, got %q", tt.expectErr, err.Error())
        }
        return
      }
      if err != nil {
        t.Fatalf("unexpected error: %v", err)
      }
      if got.worktreeName != tt.expected.worktreeName {
        t.Errorf("worktreeName = %q, want %q", got.worktreeName, tt.expected.worktreeName)
      }
      if got.identifier != tt.expected.identifier {
        t.Errorf("identifier = %q, want %q", got.identifier, tt.expected.identifier)
      }
      if got.baseBranch != tt.expected.baseBranch {
        t.Errorf("baseBranch = %q, want %q", got.baseBranch, tt.expected.baseBranch)
      }
      if got.identifierExplicit != tt.expected.identifierExplicit {
        t.Errorf("identifierExplicit = %v, want %v", got.identifierExplicit, tt.expected.identifierExplicit)
      }
    })
  }
}

func TestReadDDEVName(t *testing.T) {
  tests := []struct {
    name      string
    content   string
    expected  string
    expectErr bool
  }{
    {
      name:     "valid name field",
      content:  "name: my-project\ntype: drupal10\n",
      expected: "my-project",
    },
    {
      name:     "name with hyphens and numbers",
      content:  "name: 0001-my-project\ntype: drupal10\n",
      expected: "0001-my-project",
    },
    {
      name:     "name not on first line",
      content:  "type: drupal10\nname: my-project\nother: value\n",
      expected: "my-project",
    },
    {
      name:      "missing name field",
      content:   "type: drupal10\nother: value\n",
      expectErr: true,
    },
    {
      name:      "empty file",
      content:   "",
      expectErr: true,
    },
    {
      name:      "name-like but wrong prefix",
      content:   "project_name: foo\n",
      expectErr: true,
    },
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      dir := t.TempDir()
      path := filepath.Join(dir, "config.yaml")
      if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
        t.Fatal(err)
      }

      got, err := readDDEVName(path)
      if tt.expectErr {
        if err == nil {
          t.Fatalf("expected error, got %q", got)
        }
        return
      }
      if err != nil {
        t.Fatalf("unexpected error: %v", err)
      }
      if got != tt.expected {
        t.Errorf("readDDEVName() = %q, want %q", got, tt.expected)
      }
    })
  }

  t.Run("nonexistent file", func(t *testing.T) {
    _, err := readDDEVName("/nonexistent/path/config.yaml")
    if err == nil {
      t.Fatal("expected error for nonexistent file")
    }
  })
}

func TestGetDDEVProjectName(t *testing.T) {
  t.Run("reads from config.yaml", func(t *testing.T) {
    dir := t.TempDir()
    ddevDir := filepath.Join(dir, ".ddev")
    if err := os.MkdirAll(ddevDir, 0755); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.yaml"), []byte("name: my-project\n"), 0644); err != nil {
      t.Fatal(err)
    }

    got, err := getDDEVProjectName(dir)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if got != "my-project" {
      t.Errorf("got %q, want %q", got, "my-project")
    }
  })

  t.Run("config.local.yaml overrides config.yaml", func(t *testing.T) {
    dir := t.TempDir()
    ddevDir := filepath.Join(dir, ".ddev")
    if err := os.MkdirAll(ddevDir, 0755); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.yaml"), []byte("name: original\n"), 0644); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.local.yaml"), []byte("name: override\n"), 0644); err != nil {
      t.Fatal(err)
    }

    got, err := getDDEVProjectName(dir)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if got != "override" {
      t.Errorf("got %q, want %q", got, "override")
    }
  })

  t.Run("falls back to config.yaml when local has no name", func(t *testing.T) {
    dir := t.TempDir()
    ddevDir := filepath.Join(dir, ".ddev")
    if err := os.MkdirAll(ddevDir, 0755); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.yaml"), []byte("name: fallback\n"), 0644); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.local.yaml"), []byte("type: drupal10\n"), 0644); err != nil {
      t.Fatal(err)
    }

    got, err := getDDEVProjectName(dir)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if got != "fallback" {
      t.Errorf("got %q, want %q", got, "fallback")
    }
  })

  t.Run("error when no ddev config exists", func(t *testing.T) {
    dir := t.TempDir()
    _, err := getDDEVProjectName(dir)
    if err == nil {
      t.Fatal("expected error when no .ddev directory exists")
    }
  })

  t.Run("error when neither file has name", func(t *testing.T) {
    dir := t.TempDir()
    ddevDir := filepath.Join(dir, ".ddev")
    if err := os.MkdirAll(ddevDir, 0755); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.yaml"), []byte("type: drupal10\n"), 0644); err != nil {
      t.Fatal(err)
    }

    _, err := getDDEVProjectName(dir)
    if err == nil {
      t.Fatal("expected error when no name field in config")
    }
  })
}

func TestGetDDEVProjectType(t *testing.T) {
  tests := []struct {
    name     string
    content  string
    expected ProjectType
  }{
    {"drupal10", "name: proj\ntype: drupal10\n", ProjectDrupal},
    {"drupal11", "name: proj\ntype: drupal11\n", ProjectDrupal},
    {"drupal (generic)", "name: proj\ntype: drupal\n", ProjectDrupal},
    {"wordpress", "name: proj\ntype: wordpress\n", ProjectWordPress},
    {"wordpress variant", "name: proj\ntype: wordpress-bedrock\n", ProjectWordPress},
    {"unknown type", "name: proj\ntype: php\n", ProjectUnsupported},
    {"missing type field", "name: proj\n", ProjectUnsupported},
    {"empty file", "", ProjectUnsupported},
  }

  for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
      dir := t.TempDir()
      ddevDir := filepath.Join(dir, ".ddev")
      if err := os.MkdirAll(ddevDir, 0755); err != nil {
        t.Fatal(err)
      }
      if err := os.WriteFile(filepath.Join(ddevDir, "config.yaml"), []byte(tt.content), 0644); err != nil {
        t.Fatal(err)
      }

      got := getDDEVProjectType(dir)
      if got != tt.expected {
        t.Errorf("getDDEVProjectType() = %q, want %q", got, tt.expected)
      }
    })
  }

  t.Run("no ddev directory", func(t *testing.T) {
    dir := t.TempDir()
    got := getDDEVProjectType(dir)
    if got != ProjectUnsupported {
      t.Errorf("got %q, want %q", got, ProjectUnsupported)
    }
  })
}

func TestCreateDDEVLocalConfig(t *testing.T) {
  t.Run("creates config file with name", func(t *testing.T) {
    dir := t.TempDir()
    ddevDir := filepath.Join(dir, ".ddev")
    if err := os.MkdirAll(ddevDir, 0755); err != nil {
      t.Fatal(err)
    }

    err := createDDEVLocalConfig(dir, "t1-my-project")
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }

    content, err := os.ReadFile(filepath.Join(ddevDir, "config.local.yaml"))
    if err != nil {
      t.Fatalf("could not read created file: %v", err)
    }

    expected := "name: t1-my-project\n"
    if string(content) != expected {
      t.Errorf("file content = %q, want %q", string(content), expected)
    }
  })

  t.Run("overwrites existing file", func(t *testing.T) {
    dir := t.TempDir()
    ddevDir := filepath.Join(dir, ".ddev")
    if err := os.MkdirAll(ddevDir, 0755); err != nil {
      t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(ddevDir, "config.local.yaml"), []byte("name: old\n"), 0644); err != nil {
      t.Fatal(err)
    }

    err := createDDEVLocalConfig(dir, "new-name")
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }

    content, err := os.ReadFile(filepath.Join(ddevDir, "config.local.yaml"))
    if err != nil {
      t.Fatal(err)
    }
    if string(content) != "name: new-name\n" {
      t.Errorf("file content = %q, want %q", string(content), "name: new-name\n")
    }
  })

  t.Run("error when ddev directory missing", func(t *testing.T) {
    dir := t.TempDir()
    err := createDDEVLocalConfig(dir, "test")
    if err == nil {
      t.Fatal("expected error when .ddev directory doesn't exist")
    }
  })
}

func TestLinkProjectFiles(t *testing.T) {
  t.Run("drupal symlink created", func(t *testing.T) {
    projectRoot := t.TempDir()
    worktree := filepath.Join(projectRoot, "spaces", "main")

    // Create parent dir for the symlink target
    drupalFilesParent := filepath.Join(worktree, "web", "sites", "default")
    if err := os.MkdirAll(drupalFilesParent, 0755); err != nil {
      t.Fatal(err)
    }

    detail, err := linkProjectFiles(worktree, projectRoot, ProjectDrupal)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if !strings.Contains(detail, "Linked") {
      t.Errorf("expected detail to contain 'Linked', got %q", detail)
    }

    dest := filepath.Join(worktree, "web", "sites", "default", "files")
    target, err := os.Readlink(dest)
    if err != nil {
      t.Fatalf("expected symlink at %s: %v", dest, err)
    }

    // Verify symlink resolves to the files directory
    resolved := filepath.Join(filepath.Dir(dest), target)
    absResolved, _ := filepath.Abs(resolved)
    absExpected, _ := filepath.Abs(filepath.Join(projectRoot, "files"))
    if absResolved != absExpected {
      t.Errorf("symlink resolves to %q, want %q", absResolved, absExpected)
    }
  })

  t.Run("wordpress symlink created", func(t *testing.T) {
    projectRoot := t.TempDir()
    worktree := filepath.Join(projectRoot, "spaces", "main")

    wpParent := filepath.Join(worktree, "web", "wp-content")
    if err := os.MkdirAll(wpParent, 0755); err != nil {
      t.Fatal(err)
    }

    detail, err := linkProjectFiles(worktree, projectRoot, ProjectWordPress)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if !strings.Contains(detail, "Linked") {
      t.Errorf("expected detail to contain 'Linked', got %q", detail)
    }

    dest := filepath.Join(worktree, "web", "wp-content", "uploads")
    _, err = os.Readlink(dest)
    if err != nil {
      t.Fatalf("expected symlink at %s: %v", dest, err)
    }
  })

  t.Run("unsupported project type skipped", func(t *testing.T) {
    projectRoot := t.TempDir()
    worktree := filepath.Join(projectRoot, "spaces", "main")

    detail, err := linkProjectFiles(worktree, projectRoot, ProjectUnsupported)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if !strings.Contains(detail, "Skipped") {
      t.Errorf("expected 'Skipped', got %q", detail)
    }
  })

  t.Run("existing correct symlink is idempotent", func(t *testing.T) {
    projectRoot := t.TempDir()
    worktree := filepath.Join(projectRoot, "spaces", "main")

    drupalFilesParent := filepath.Join(worktree, "web", "sites", "default")
    if err := os.MkdirAll(drupalFilesParent, 0755); err != nil {
      t.Fatal(err)
    }

    // First call creates the symlink
    _, err := linkProjectFiles(worktree, projectRoot, ProjectDrupal)
    if err != nil {
      t.Fatalf("first call: %v", err)
    }

    // Second call should detect existing symlink
    detail, err := linkProjectFiles(worktree, projectRoot, ProjectDrupal)
    if err != nil {
      t.Fatalf("second call: %v", err)
    }
    if !strings.Contains(detail, "already exists") {
      t.Errorf("expected 'already exists', got %q", detail)
    }
  })

  t.Run("existing directory is replaced by symlink", func(t *testing.T) {
    projectRoot := t.TempDir()
    worktree := filepath.Join(projectRoot, "spaces", "main")

    dest := filepath.Join(worktree, "web", "sites", "default", "files")
    if err := os.MkdirAll(dest, 0755); err != nil {
      t.Fatal(err)
    }
    // Put a file inside so it's not empty
    if err := os.WriteFile(filepath.Join(dest, "test.txt"), []byte("hi"), 0644); err != nil {
      t.Fatal(err)
    }

    detail, err := linkProjectFiles(worktree, projectRoot, ProjectDrupal)
    if err != nil {
      t.Fatalf("unexpected error: %v", err)
    }
    if !strings.Contains(detail, "Linked") {
      t.Errorf("expected 'Linked', got %q", detail)
    }

    // Verify it's now a symlink
    _, err = os.Readlink(dest)
    if err != nil {
      t.Fatalf("expected symlink at %s: %v", dest, err)
    }
  })
}

func TestPrintSummary(t *testing.T) {
  // Capture stdout
  oldStdout := os.Stdout
  r, w, _ := os.Pipe()
  os.Stdout = w

  steps := []StepResult{
    {Description: "Created worktree", Detail: "my-feature"},
    {Description: "DDEV", Detail: "Started"},
  }
  printSummary(steps)

  w.Close()
  os.Stdout = oldStdout

  var buf bytes.Buffer
  io.Copy(&buf, r)
  output := buf.String()

  if !strings.Contains(output, "Workspace Setup Complete") {
    t.Errorf("expected output to contain 'Workspace Setup Complete', got %q", output)
  }
  if !strings.Contains(output, "Created worktree:") {
    t.Errorf("expected output to contain 'Created worktree:', got %q", output)
  }
  if !strings.Contains(output, "my-feature") {
    t.Errorf("expected output to contain 'my-feature', got %q", output)
  }
  if !strings.Contains(output, "DDEV:") {
    t.Errorf("expected output to contain 'DDEV:', got %q", output)
  }
  if !strings.Contains(output, "Started") {
    t.Errorf("expected output to contain 'Started', got %q", output)
  }
}
