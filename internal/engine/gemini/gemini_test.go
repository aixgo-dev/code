package gemini

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aixgo-dev/code/internal/domain"
)

func writeFakeGemini(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gemini")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

const echoArgs = `#!/bin/sh
echo "$@"
exit 0
`

func TestRunPassesThePromptAndModel(t *testing.T) {
	r := New()
	r.Bin = writeFakeGemini(t, echoArgs)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(),
		Prompt: "do the thing", Model: "gemini-3.5-flash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"-p", "do the thing", "-m", "gemini-3.5-flash"} {
		if !strings.Contains(res.Summary, want) {
			t.Fatalf("args missing %q: %s", want, res.Summary)
		}
	}
}

// aixgo-code always runs unattended, so a permission prompt would hang the
// job. -y is the Gemini CLI equivalent of Codex's workspace-write sandbox:
// the human gate is the pull request, not each tool call.
func TestRunAutoApprovesActions(t *testing.T) {
	r := New()
	r.Bin = writeFakeGemini(t, echoArgs)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Summary, "-y") {
		t.Fatalf("unattended runs must pass -y: %s", res.Summary)
	}
}

const echoGoEnv = `#!/bin/sh
printf '%s\n%s\n' "$GOCACHE" "$GOPATH"
exit 0
`

func TestRunSetsWorkspaceLocalGoCaches(t *testing.T) {
	work := t.TempDir()
	r := New()
	r.Bin = writeFakeGemini(t, echoGoEnv)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: work, Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(res.Summary, "\n")
	if len(lines) != 2 {
		t.Fatalf("summary = %q, want GOCACHE and GOPATH", res.Summary)
	}
	if !strings.HasPrefix(lines[0], work) || !strings.HasSuffix(lines[0], ".gocache") {
		t.Fatalf("GOCACHE = %q, want a .gocache under %q", lines[0], work)
	}
	if !strings.HasPrefix(lines[1], work) || !strings.HasSuffix(lines[1], ".gopath") {
		t.Fatalf("GOPATH = %q, want a .gopath under %q", lines[1], work)
	}
}

const echoVertexEnv = `#!/bin/sh
printf '%s\n%s\n%s\n%s\n' "$GOOGLE_GENAI_USE_VERTEXAI" "$GOOGLE_CLOUD_PROJECT" "$GOOGLE_CLOUD_LOCATION" "$GOOGLE_API_KEY"
exit 0
`

func TestRunExportsVertexSettingsForTheGeminiCLI(t *testing.T) {
	t.Setenv("AIXGO_VERTEX_PROJECT", "simplycubed-agents")
	t.Setenv("AIXGO_VERTEX_LOCATION", "us-central1")
	t.Setenv("AIXGO_VERTEX_API_KEY", "vertex-key")
	r := New()
	r.Bin = writeFakeGemini(t, echoVertexEnv)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Split(res.Summary, "\n")
	if len(got) != 4 {
		t.Fatalf("summary = %q", res.Summary)
	}
	if got[0] != "true" || got[1] != "simplycubed-agents" || got[2] != "us-central1" || got[3] != "vertex-key" {
		t.Fatalf("vertex env = %#v", got)
	}
}

const echoADC = `#!/bin/sh
printf '%s\n%s\n' "$GOOGLE_API_KEY" "$GOOGLE_APPLICATION_CREDENTIALS"
if [ -n "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
  cat "$GOOGLE_APPLICATION_CREDENTIALS"
fi
exit 0
`

const saJSON = `{"type":"service_account","project_id":"simplycubed-agents","client_email":"vertex@simplycubed-agents.iam.gserviceaccount.com"}`

func TestRunTreatsServiceAccountJSONAsADC(t *testing.T) {
	work := t.TempDir()
	t.Setenv("AIXGO_VERTEX_PROJECT", "simplycubed-agents")
	t.Setenv("AIXGO_VERTEX_API_KEY", saJSON)
	r := New()
	r.Bin = writeFakeGemini(t, echoADC)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: work, Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.SplitN(res.Summary, "\n", 2)
	if len(lines) < 2 {
		t.Fatalf("summary = %q", res.Summary)
	}
	creds := lines[0]
	if creds == "" || strings.HasPrefix(creds, "{") {
		t.Fatalf("GOOGLE_APPLICATION_CREDENTIALS must be a file path, not the JSON, got %q", creds)
	}
	if strings.HasPrefix(creds, work) {
		t.Fatalf("the key file must not live in the worktree, got %q", creds)
	}
	if !strings.Contains(lines[1], `"type":"service_account"`) {
		t.Fatalf("the key file should contain the service account JSON, got %q", res.Summary)
	}
}

func TestRunDefaultsLocationWhenUnset(t *testing.T) {
	t.Setenv("AIXGO_VERTEX_PROJECT", "p")
	t.Setenv("AIXGO_VERTEX_LOCATION", "")
	t.Setenv("AIXGO_VERTEX_API_KEY", "k")
	r := New()
	r.Bin = writeFakeGemini(t, echoVertexEnv)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Split(res.Summary, "\n")
	if len(got) != 4 || got[2] != "us-central1" {
		t.Fatalf("default location missing: %q", res.Summary)
	}
}

const echoTrustEnv = `#!/bin/sh
printf '%s\n' "$GEMINI_CLI_TRUST_WORKSPACE"
exit 0
`

func TestRunTrustsWorkspaceForHeadlessCI(t *testing.T) {
	r := New()
	r.Bin = writeFakeGemini(t, echoTrustEnv)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Summary != "true" {
		t.Fatalf("GEMINI_CLI_TRUST_WORKSPACE = %q, want true", res.Summary)
	}
}

func TestRunPreservesCallerTrustWorkspaceOverride(t *testing.T) {
	r := New()
	r.Bin = writeFakeGemini(t, echoTrustEnv)
	r.ExtraEnv = []string{"GEMINI_CLI_TRUST_WORKSPACE=false"}
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Summary != "false" {
		t.Fatalf("caller override lost: %q", res.Summary)
	}
}

func TestRunReportsAFailureAsAnEngineError(t *testing.T) {
	r := New()
	r.Bin = writeFakeGemini(t, "#!/bin/sh\necho boom >&2\nexit 3\n")
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err == nil || res.Err == nil {
		t.Fatal("a non-zero exit must surface as an engine error")
	}
	if !strings.Contains(res.Summary, "boom") {
		t.Fatalf("the CLI's own output should be kept: %q", res.Summary)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("engine error should include CLI output: %v", err)
	}
}

func TestRunRequiresAWorkDir(t *testing.T) {
	if _, err := New().Run(context.Background(), domain.RunRequest{Role: domain.RoleImplementer}); err == nil {
		t.Fatal("expected an error with no WorkDir")
	}
}
