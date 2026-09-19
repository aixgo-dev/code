package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func stubGHForInit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	prevTTY := initStdinIsTTY
	initStdinIsTTY = func() bool { return false }
	t.Cleanup(func() { initStdinIsTTY = prevTTY })
	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "gh.log")
	stubPath := filepath.Join(stubDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestInitEngineFlagWritesConfigAndCaller(t *testing.T) {
	stubGHForInit(t)
	buildVersionForTest(t, "0.1.0")

	for _, tc := range []struct {
		engine          string
		wantConfig      string
		wantInCaller    []string
		missingInCaller []string
		wantNext        []string
		missingNext     []string
	}{
		{
			engine:     "codex",
			wantConfig: "engine: codex",
			wantInCaller: []string{
				"azure-openai-endpoint: ${{ vars.AIXGO_AZURE_OPENAI_ENDPOINT }}",
				"azure-openai-api-key: ${{ secrets.AIXGO_AZURE_OPENAI_API_KEY }}",
			},
			missingInCaller: []string{"vertex-project", "vertex-api-key", "Claude on Actions"},
			wantNext: []string{
				"AIXGO_AZURE_OPENAI_ENDPOINT",
				"AIXGO_AZURE_OPENAI_API_KEY",
				"AIXGO_GH_APP_CLIENT_ID",
				"AIXGO_GH_APP_PRIVATE_KEY",
			},
			missingNext: []string{"AIXGO_VERTEX_PROJECT", "aixgo-dev/code#147"},
		},
		{
			engine:     "gemini",
			wantConfig: "engine: gemini",
			wantInCaller: []string{
				"vertex-project: ${{ vars.AIXGO_VERTEX_PROJECT }}",
				"vertex-location: ${{ vars.AIXGO_VERTEX_LOCATION }}",
				"vertex-api-key: ${{ secrets.AIXGO_VERTEX_API_KEY }}",
			},
			missingInCaller: []string{"azure-openai-endpoint", "azure-openai-api-key"},
			wantNext: []string{
				"AIXGO_VERTEX_PROJECT",
				"AIXGO_VERTEX_API_KEY",
				"AIXGO_GH_APP_CLIENT_ID",
				"AIXGO_GH_APP_PRIVATE_KEY",
			},
			missingNext: []string{"AIXGO_AZURE_OPENAI_ENDPOINT"},
		},
		{
			engine:     "claude",
			wantConfig: "engine: claude",
			wantInCaller: []string{
				"Claude on Actions is not wired yet (aixgo-dev/code#147)",
				"github-app-private-key: ${{ secrets.AIXGO_GH_APP_PRIVATE_KEY }}",
			},
			missingInCaller: []string{"azure-openai-endpoint", "vertex-project", "azure-openai-api-key", "vertex-api-key"},
			wantNext: []string{
				"AIXGO_GH_APP_CLIENT_ID",
				"AIXGO_GH_APP_PRIVATE_KEY",
				"aixgo-dev/code#147",
			},
			missingNext: []string{"AIXGO_AZURE_OPENAI_ENDPOINT", "AIXGO_VERTEX_PROJECT"},
		},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			repoDir := t.TempDir()
			var out bytes.Buffer
			if err := initCmd([]string{
				"--repo-dir", repoDir,
				"--workflow",
				"--app-name", "acme-code",
				"--engine", tc.engine,
			}, &out); err != nil {
				t.Fatalf("initCmd: %v", err)
			}
			cfg, err := os.ReadFile(filepath.Join(repoDir, ".github", "aixgo.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(cfg), tc.wantConfig) {
				t.Fatalf("config missing %q:\n%s", tc.wantConfig, cfg)
			}
			workflow, err := os.ReadFile(filepath.Join(repoDir, ".github", "workflows", "aixgo.yml"))
			if err != nil {
				t.Fatal(err)
			}
			body := string(workflow)
			for _, want := range tc.wantInCaller {
				if !strings.Contains(body, want) {
					t.Fatalf("caller missing %q:\n%s", want, body)
				}
			}
			for _, miss := range tc.missingInCaller {
				if strings.Contains(body, miss) {
					t.Fatalf("caller should not contain %q:\n%s", miss, body)
				}
			}
			output := out.String()
			if strings.Contains(output, "using engine: codex (default") {
				t.Fatalf("explicit --engine should not print the non-TTY default message:\n%s", output)
			}
			for _, want := range tc.wantNext {
				if !strings.Contains(output, want) {
					t.Fatalf("next steps missing %q:\n%s", want, output)
				}
			}
			for _, miss := range tc.missingNext {
				if strings.Contains(output, miss) {
					t.Fatalf("next steps should not mention %q:\n%s", miss, output)
				}
			}
		})
	}
}

func TestInitNonTTYDefaultsToCodex(t *testing.T) {
	stubGHForInit(t)
	prev := initStdinIsTTY
	initStdinIsTTY = func() bool { return false }
	t.Cleanup(func() { initStdinIsTTY = prev })

	repoDir := t.TempDir()
	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir, "--app-name", "acme-code"}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(repoDir, ".github", "aixgo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "engine: codex") {
		t.Fatalf("expected engine: codex:\n%s", cfg)
	}
	if !strings.Contains(out.String(), "using engine: codex (default; pass --engine or run interactively to choose)") {
		t.Fatalf("expected default-engine message:\n%s", out.String())
	}
}

func TestInitInteractiveMenuSelectsGemini(t *testing.T) {
	stubGHForInit(t)
	prevTTY := initStdinIsTTY
	prevIn := initPromptIn
	initStdinIsTTY = func() bool { return true }
	initPromptIn = strings.NewReader("2\n")
	t.Cleanup(func() {
		initStdinIsTTY = prevTTY
		initPromptIn = prevIn
	})

	repoDir := t.TempDir()
	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir, "--workflow", "--app-name", "acme-code"}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(repoDir, ".github", "aixgo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "engine: gemini") {
		t.Fatalf("expected engine: gemini:\n%s", cfg)
	}
	workflow, err := os.ReadFile(filepath.Join(repoDir, ".github", "workflows", "aixgo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "vertex-project:") {
		t.Fatalf("expected vertex inputs:\n%s", workflow)
	}
	if strings.Contains(string(workflow), "azure-openai-endpoint") {
		t.Fatalf("gemini caller must not wire Azure:\n%s", workflow)
	}
	if !strings.Contains(out.String(), "Select the model engine:") {
		t.Fatalf("expected interactive menu:\n%s", out.String())
	}
}

func TestInitForceOverwritesEngine(t *testing.T) {
	stubGHForInit(t)
	buildVersionForTest(t, "0.1.0")
	repoDir := t.TempDir()

	var first bytes.Buffer
	if err := initCmd([]string{
		"--repo-dir", repoDir, "--workflow", "--app-name", "acme-code", "--engine", "codex",
	}, &first); err != nil {
		t.Fatalf("first init: %v", err)
	}
	var second bytes.Buffer
	if err := initCmd([]string{
		"--repo-dir", repoDir, "--workflow", "--app-name", "acme-code", "--engine", "gemini", "--force",
	}, &second); err != nil {
		t.Fatalf("force init: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(repoDir, ".github", "aixgo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "engine: gemini") {
		t.Fatalf("force should rewrite engine:\n%s", cfg)
	}
	workflow, err := os.ReadFile(filepath.Join(repoDir, ".github", "workflows", "aixgo.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(workflow)
	if !strings.Contains(body, "vertex-project:") || strings.Contains(body, "azure-openai-endpoint") {
		t.Fatalf("force should rewrite caller to gemini:\n%s", body)
	}
	if !strings.Contains(second.String(), "wrote ") {
		t.Fatalf("force should report writes:\n%s", second.String())
	}
}

func TestNormalizeEngine(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"codex", "Claude", " GEMINI "} {
		if _, err := normalizeEngine(in); err != nil {
			t.Fatalf("normalizeEngine(%q): %v", in, err)
		}
	}
	if _, err := normalizeEngine("gpt"); err == nil {
		t.Fatal("unknown engine must error")
	}
}

func TestRenderCallerWorkflowOmitsOtherEngines(t *testing.T) {
	t.Parallel()
	codex := renderCallerWorkflow("0.5.0", "acme", "codex")
	gemini := renderCallerWorkflow("0.5.0", "acme", "gemini")
	claude := renderCallerWorkflow("0.5.0", "acme", "claude")
	if !strings.Contains(codex, "azure-openai-endpoint") || strings.Contains(codex, "vertex-project") {
		t.Fatalf("codex caller:\n%s", codex)
	}
	if !strings.Contains(gemini, "vertex-project") || strings.Contains(gemini, "azure-openai") {
		t.Fatalf("gemini caller:\n%s", gemini)
	}
	if strings.Contains(claude, "azure-openai") || strings.Contains(claude, "vertex-") {
		t.Fatalf("claude caller:\n%s", claude)
	}
	if !strings.Contains(claude, "aixgo-dev/code#147") {
		t.Fatalf("claude caller should document Actions limitation:\n%s", claude)
	}
}
