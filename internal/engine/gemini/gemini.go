// Package gemini implements engine.Runner by shelling out to the Gemini CLI
// (`gemini -p`) pointed at Vertex AI.
//
// It follows the same two lessons as the other adapters (see docs/decisions/0003):
//
//   - The environment is pre-configured so the agent fixes the bug rather than
//     the environment. Workspace-local Go caches are set here for the same
//     reason they are set there.
//   - This adapter runs the turn and nothing else. It does not police what the
//     agent changed; that is the gate's job and the PR guard's job.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aixgo-dev/code/internal/domain"
)

const defaultLocation = "us-central1"

// Runner runs one role turn via `gemini -p`.
type Runner struct {
	// Bin is the gemini binary. Defaults to "gemini" (resolved on PATH).
	Bin string
	// Model is the model to request. Empty uses the CLI's own default.
	Model string
	// ExtraEnv is appended to the child environment, for pre-configuring the
	// workspace. See the package doc.
	ExtraEnv []string
}

// New returns a Runner with the defaults applied.
func New() *Runner { return &Runner{} }

func (r *Runner) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	return "gemini"
}

// Run executes one turn. A non-nil error means the CLI itself failed; it does
// not mean the gate failed, which is a separate signal the loop grades.
func (r *Runner) Run(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
	if req.WorkDir == "" {
		err := errors.New("gemini: WorkDir is required")
		return domain.RunResult{Role: req.Role, Err: err}, err
	}

	credsPath, cleanup, err := materializeVertexCreds()
	if err != nil {
		return domain.RunResult{Role: req.Role, Err: err}, err
	}
	defer cleanup()

	args := []string{"-p", req.Prompt, "--output-format", "text", "-y"}
	model := req.Model
	if model == "" {
		model = r.Model
	}
	if model != "" {
		args = append(args, "-m", model)
	}

	cmd := exec.CommandContext(ctx, r.bin(), args...)
	cmd.Dir = req.WorkDir
	cmd.Env = r.childEnv(req.WorkDir, credsPath)
	out, runErr := cmd.CombinedOutput()

	res := domain.RunResult{Role: req.Role, Summary: tail(strings.TrimSpace(string(out)), 40)}
	if runErr != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			res.Err = fmt.Errorf("gemini -p: %w\n%s", runErr, tail(msg, 20))
		} else {
			res.Err = fmt.Errorf("gemini -p: %w", runErr)
		}
		return res, res.Err
	}
	return res, nil
}

// childEnv builds the child environment. It mirrors the other adapters for the
// Go caches, and translates the AIXGO_VERTEX_* names into the names the Gemini
// CLI reads for Vertex.
func (r *Runner) childEnv(workDir, credsPath string) []string {
	env := filterEnvKeys(os.Environ(),
		"GOCACHE", "GOPATH", "GH_TOKEN", "GITHUB_TOKEN",
		"GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION",
		"GOOGLE_API_KEY", "GOOGLE_APPLICATION_CREDENTIALS",
	)
	env = append(env, r.ExtraEnv...)
	if !hasEnvKey(r.ExtraEnv, "GOCACHE") {
		gc := filepath.Join(workDir, ".gocache")
		_ = os.MkdirAll(gc, 0o755)
		env = append(env, "GOCACHE="+gc)
	}
	if !hasEnvKey(r.ExtraEnv, "GOPATH") {
		gp := filepath.Join(workDir, ".gopath")
		_ = os.MkdirAll(gp, 0o755)
		env = append(env, "GOPATH="+gp)
	}
	env = append(env, "GOOGLE_GENAI_USE_VERTEXAI=true")
	if p := strings.TrimSpace(os.Getenv("AIXGO_VERTEX_PROJECT")); p != "" {
		env = append(env, "GOOGLE_CLOUD_PROJECT="+p)
	}
	loc := strings.TrimSpace(os.Getenv("AIXGO_VERTEX_LOCATION"))
	if loc == "" {
		loc = defaultLocation
	}
	env = append(env, "GOOGLE_CLOUD_LOCATION="+loc)
	if credsPath != "" {
		env = append(env, "GOOGLE_APPLICATION_CREDENTIALS="+credsPath)
	} else if k := strings.TrimSpace(os.Getenv("AIXGO_VERTEX_API_KEY")); k != "" {
		env = append(env, "GOOGLE_API_KEY="+k)
	}
	// gemini-cli 0.60+ defaults folder trust on; headless CI checkouts are
	// untrusted and exit 55 (FatalUntrustedWorkspaceError). -y does not bypass
	// that gate — mirror unattended CI by trusting the workspace unless the
	// caller already set the override.
	if !hasEnvKey(r.ExtraEnv, "GEMINI_CLI_TRUST_WORKSPACE") {
		env = append(env, "GEMINI_CLI_TRUST_WORKSPACE=true")
	}
	return env
}

// materializeVertexCreds writes a service-account JSON from AIXGO_VERTEX_API_KEY
// to a temp file outside the worktree. A plain API key is left in the env and
// handled by childEnv. The file is 0600 and must be removed by the caller.
func materializeVertexCreds() (string, func(), error) {
	noop := func() {}
	raw := strings.TrimSpace(os.Getenv("AIXGO_VERTEX_API_KEY"))
	if raw == "" || !isServiceAccountJSON(raw) {
		return "", noop, nil
	}
	f, err := os.CreateTemp("", "aixgo-vertex-sa-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("gemini: write service account key: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := f.WriteString(raw); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, fmt.Errorf("gemini: write service account key: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("gemini: write service account key: %w", err)
	}
	return path, cleanup, nil
}

func isServiceAccountJSON(s string) bool {
	var doc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return false
	}
	return doc.Type == "service_account"
}

func filterEnvKeys(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(e, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}

func hasEnvKey(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func tail(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
