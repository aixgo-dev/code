package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aixgo-dev/code/internal/app"
	"github.com/aixgo-dev/code/internal/buildinfo"
	"github.com/aixgo-dev/code/internal/command"
	"github.com/aixgo-dev/code/internal/config"
	"github.com/aixgo-dev/code/internal/domain"
	"github.com/aixgo-dev/code/internal/engine"
	"github.com/aixgo-dev/code/internal/engine/claude"
	"github.com/aixgo-dev/code/internal/engine/codex"
	"github.com/aixgo-dev/code/internal/engine/gemini"
	forge2 "github.com/aixgo-dev/code/internal/forge"
	"github.com/aixgo-dev/code/internal/forge/dryrun"
	forgegh "github.com/aixgo-dev/code/internal/forge/gh"
	"github.com/aixgo-dev/code/internal/loop"
	vcsgit "github.com/aixgo-dev/code/internal/vcs/git"
	"github.com/aixgo-dev/code/internal/worktree"
)

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "error:", err)
		if errors.Is(err, ErrConfigMissing) {
			return 3
		}
		return 1
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, buildinfo.Version)
	case "init":
		if err := initCmd(args[1:], stdout); err != nil {
			return fail(err)
		}
	case "command":
		if err := commandCmd(args[1:], stdout); err != nil {
			return fail(err)
		}
	case "preflight":
		if err := preflightCmd(args[1:], stdout); err != nil {
			return fail(err)
		}
	case "run":
		if err := runCmd(args[1:]); err != nil {
			return fail(err)
		}
	case "address":
		if err := addressCmd(args[1:]); err != nil {
			return fail(err)
		}
	default:
		usage(stderr)
		return 2
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `aixgo %s

usage:
  aixgo-code version
  aixgo-code init [--repo-dir .] [--workflow] [--engine codex|claude|gemini] [--force]
  aixgo-code preflight [--repo-dir .]
  aixgo-code command <owner/repo#N> --body "<comment>" [flags]
  aixgo-code run <owner/repo#N> [flags]
  aixgo-code address <owner/repo#PR> [flags]

run drives an issue to a pull request. address runs the fix-on-request loop
over an open pull request: it reads the human's review feedback and pushes a
fix back to the same branch, or reports that there is nothing new to address.

flags (both commands):
  --repo-dir    path to the target repo checkout (default ".")
  --actor       login that triggered the run; refused unless it has write access
  --dry-run     run the whole loop but make no GitHub writes and no push
  --model       engine model/deployment name (default "gpt-5.4")
  --base        worktree base ref (default "origin/HEAD")
  --state-dir   where worktrees and the generated config live

environment (engine settings; never committed to a repo):
  AIXGO_AZURE_OPENAI_ENDPOINT   e.g. https://<resource>.openai.azure.com (codex)
  AIXGO_AZURE_OPENAI_API_KEY    the key, read by name and never written to disk
  AIXGO_VERTEX_PROJECT          Google Cloud project (gemini / Vertex)
  AIXGO_VERTEX_LOCATION         Vertex location, default us-central1
  AIXGO_VERTEX_API_KEY          Vertex API key, read by name and never written to disk
`, buildinfo.Version)
}

const starterConfig = `# Repository-local contract for Aixgo Code.
# Keep the label prefix unless you already run another ax:* lifecycle.
labelPrefix: ax

# The GitHub App you created and installed, without the "[bot]" suffix. Comment
# commands address it: "@__AIXGO_APP_NAME__ go" on an issue starts work.
#
# This is yours, not ours. App names are globally unique, so every installation
# has a different one, and addressing your real bot is what makes GitHub offer
# it in the autocomplete after someone types "@".
#
# The caller workflow triggers on this same handle. If you rename the App,
# change it here and re-run "aixgo-code init --workflow"; preflight fails if
# the two ever disagree.
appName: __AIXGO_APP_NAME__

# Model engine: codex (Azure OpenAI, default), gemini (Vertex AI), or claude
# (local CLI today; Actions install tracked in aixgo-dev/code#147).
# Chosen by "aixgo-code init --engine" or the interactive menu.
engine: __AIXGO_ENGINE__

# Required. Fill this in with the real gate that is already green on main.
gate:

# Optional. One-time setup command before each run (install deps, generate code).
# setup:

# Optional. Disable the generated commit/PR attribution marker.
# attribution: false

# Optional. Ask the agent to add a generated walkthrough and changes table to PRs.
# prDescription: rich
`

const (
	latestKnownWorkflowTag     = "v0.5.0"
	callerWorkflowTagToken     = "__AIXGO_TAG__"
	callerWorkflowAppNameToken = "__AIXGO_APP_NAME__"
)

//go:embed aixgo-caller.yml.tmpl
var callerWorkflowTemplate string

//go:embed aixgo-selftest.yml.tmpl
var selftestWorkflowTemplate string

func usesAzure(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	switch cfg.Engine {
	case "claude", "gemini":
		return false
	}
	return true
}

func engineEnv(cfg *config.Config) (string, error) {
	if cfg != nil && cfg.Engine == "claude" {
		return "", nil
	}
	if cfg != nil && cfg.Engine == "gemini" {
		if err := requireSet("AIXGO_VERTEX_PROJECT", sectionVariable); err != nil {
			return "", err
		}
		if err := requireSet("AIXGO_VERTEX_API_KEY", sectionSecret); err != nil {
			return "", err
		}
		return "", nil
	}
	if err := requireSet("AIXGO_AZURE_OPENAI_ENDPOINT", sectionVariable); err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(os.Getenv("AIXGO_AZURE_OPENAI_ENDPOINT"), "/")
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("AIXGO_AZURE_OPENAI_ENDPOINT is not a valid URL: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("AIXGO_AZURE_OPENAI_ENDPOINT must be an https URL like https://<resource>.openai.azure.com, got %q", endpoint)
	}
	if err := requireSet("AIXGO_AZURE_OPENAI_API_KEY", sectionSecret); err != nil {
		return "", err
	}
	return endpoint, nil
}

var ErrConfigMissing = errors.New("configuration missing")

type section string

const (
	sectionVariable section = "variable"
	sectionSecret   section = "secret"
)

func requireSet(name string, where section) error {
	if strings.TrimSpace(os.Getenv(name)) != "" {
		return nil
	}
	return fmt.Errorf("%w: %s is not set. It is a repository %s on your own repository, under Settings > Secrets and variables > Actions; a reusable workflow never inherits %ss from Aixgo", ErrConfigMissing, name, where, where)
}

func newVCS(self string) *vcsgit.Git {
	if self == "" {
		return &vcsgit.Git{}
	}
	return &vcsgit.Git{
		AuthorName:  self,
		AuthorEmail: fmt.Sprintf("%s@users.noreply.github.com", strings.ReplaceAll(self, "[bot]", "")),
	}
}

func newRunner(cfg *config.Config, codexHome, model string) engine.Runner {
	if cfg.Engine == "claude" {
		return &claude.Runner{Model: model}
	}
	if cfg.Engine == "gemini" {
		if model == "" || model == "gpt-5.4" {
			model = "gemini-2.5-pro"
		}
		return &gemini.Runner{Model: model}
	}
	r := codex.New(codexHome)
	if mode := os.Getenv("AIXGO_SANDBOX"); mode != "" {
		r.Sandbox = mode
	}
	return r
}

func takeBody(argv []string) (body string, rest []string, err error) {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--body" || a == "-body":
			if i+1 >= len(argv) {
				return "", nil, fmt.Errorf("--body requires a value")
			}
			body = argv[i+1]
			i++
		case strings.HasPrefix(a, "--body="), strings.HasPrefix(a, "-body="):
			_, v, _ := strings.Cut(a, "=")
			body = v
		default:
			rest = append(rest, a)
		}
	}
	return body, rest, nil
}

func appNameFor(argv []string) (string, error) {
	repoDir := "."
	for i, a := range argv {
		switch {
		case a == "--repo-dir" || a == "-repo-dir":
			if i+1 < len(argv) {
				repoDir = argv[i+1]
			}
		case strings.HasPrefix(a, "--repo-dir="), strings.HasPrefix(a, "-repo-dir="):
			_, v, _ := strings.Cut(a, "=")
			repoDir = v
		}
	}
	cfg, err := config.Load(filepath.Join(repoDir, ".github", "aixgo.yml"))
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	return cfg.AppName, nil
}

func commandCmd(argv []string, stdout io.Writer) error {
	body, rest, err := takeBody(argv)
	if err != nil {
		return err
	}
	appName, err := appNameFor(rest)
	if err != nil {
		return err
	}
	kind := command.Parse(body, appName)

	switch kind {
	case command.Help:
		return reply(rest, command.HelpTextFor(appName), stdout)
	case command.Go, command.Address:
		a, err := newAnswerer(rest)
		if err != nil {
			return err
		}
		if a.ok && command.Misdirected(kind, a.onPR) {
			return a.post(command.MisdirectedText(kind, a.onPR, appName), stdout)
		}
		if kind == command.Go {
			return runCmd(rest)
		}
		return addressCmd(rest)
	default:
		if command.Addressed(body, appName) {
			return reply(rest, command.UnknownTextFor(appName), stdout)
		}
		fmt.Fprintln(stdout, "no command recognised in that comment; nothing to do")
		return nil
	}
}

var newCommentForge = func() forge2.Forge { return &forgegh.Forge{} }

type answerer struct {
	forge forge2.Forge
	dry   *dryrun.Forge
	ref   domain.Issue
	onPR  bool
	ok bool
}

func newAnswerer(argv []string) (answerer, error) {
	var a answerer
	for _, arg := range argv {
		if ref, err := app.ParseIssueRef(arg); err == nil {
			a.ref, a.ok = ref, true
			break
		}
	}
	if !a.ok {
		return a, nil
	}
	gf := newCommentForge()
	a.forge = gf
	if dryRunRequested(argv) {
		a.dry = dryrun.New(gf)
		a.forge = a.dry
	}
	onPR, err := a.forge.IsPullRequest(context.Background(), a.ref.Repo, a.ref.Number)
	if err != nil {
		return a, fmt.Errorf("resolve %s#%d: %w", a.ref.Repo, a.ref.Number, err)
	}
	a.onPR = onPR
	return a, nil
}

func (a answerer) post(body string, stdout io.Writer) error {
	fmt.Fprintln(stdout, body)
	if !a.ok {
		return nil
	}
	defer func() {
		if a.dry == nil {
			return
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, a.dry.Report())
	}()
	ctx := context.Background()
	if a.onPR {
		return a.forge.CommentPR(ctx, a.ref.Repo, a.ref.Number, body)
	}
	return a.forge.Comment(ctx, a.ref.Repo, a.ref.Number, body)
}

func dryRunRequested(argv []string) bool {
	if os.Getenv("AIXGO_DRY_RUN") != "" {
		return true
	}
	for _, arg := range argv {
		switch arg {
		case "--dry-run", "-dry-run", "--dry-run=true", "-dry-run=true":
			return true
		}
	}
	return false
}

func reply(argv []string, body string, stdout io.Writer) error {
	a, err := newAnswerer(argv)
	if err != nil {
		return err
	}
	return a.post(body, stdout)
}

func checkMentionAgreement(repoDir, appName string) error {
	dir := filepath.Join(repoDir, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	want := "'" + command.MentionFor(appName) + "'"
	var callers []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), "aixgo-dev/code/.github/workflows/aixgo.yml@") {
			continue
		}
		callers = append(callers, path)
		if strings.Contains(string(b), want) {
			return nil
		}
	}
	if len(callers) == 0 {
		return nil
	}
	return fmt.Errorf("%w: .github/aixgo.yml sets appName %q, but %s does not trigger on %s. Comment commands will never fire. Re-run \"aixgo-code init --workflow\" to rewrite the trigger from the config",
		ErrConfigMissing, appName, strings.Join(callers, ", "), want)
}

func preflightCmd(argv []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	actions := fs.Bool("actions", false, "also check the values only a caller workflow supplies")
	if _, err := parseInterleaved(fs, argv); err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(*repoDir, ".github", "aixgo.yml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if _, err := engineEnv(cfg); err != nil {
		return err
	}
	if cfg.AppName == "" {
		return fmt.Errorf("%w: .github/aixgo.yml sets no appName. Comment commands address the App you installed, so without it none of them can fire. Add \"appName: <your-app>\" or re-run \"aixgo-code init --workflow --app-name <your-app>\"", ErrConfigMissing)
	}
	if err := checkMentionAgreement(*repoDir, cfg.AppName); err != nil {
		return err
	}
	if *actions {
		for _, v := range []struct {
			name  string
			where section
		}{
			{"AIXGO_GH_APP_CLIENT_ID", sectionVariable},
			{"AIXGO_GH_APP_PRIVATE_KEY", sectionSecret},
		} {
			if err := requireSet(v.name, v.where); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(stdout, "preflight ok: config and engine settings are present")
	return nil
}

type commonFlags struct {
	repoDir string
	base    string
	actor   string
	dry  *dryrun.Forge
	cfg  *config.Config
	deps app.Deps
}

func parseInterleaved(fs *flag.FlagSet, argv []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(argv); err != nil {
			return nil, err
		}
		argv = fs.Args()
		if len(argv) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, argv[0])
		argv = argv[1:]
	}
}

func isBotLogin(login string) bool {
	return strings.HasSuffix(login, "[bot]")
}

func prepare(name string, argv []string) (*commonFlags, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	model := fs.String("model", "gpt-5.4", "engine model/deployment name")
	base := fs.String("base", "origin/HEAD", "worktree base ref")
	stateDir := fs.String("state-dir", filepath.Join(os.TempDir(), "aixgo"), "state directory")
	actor := fs.String("actor", "", "login that triggered this run; checked for write access")
	dryRun := fs.Bool("dry-run", false, "run the whole loop but make no GitHub writes and no push")
	rest, err := parseInterleaved(fs, argv)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := config.Load(filepath.Join(*repoDir, ".github", "aixgo.yml"))
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	endpoint, err := engineEnv(cfg)
	if err != nil {
		return nil, nil, err
	}

	codexHome := filepath.Join(*stateDir, "codex-home")
	if usesAzure(cfg) {
		if _, err := codex.WriteConfig(codexHome, codex.ProviderConfig{
			Model:   *model,
			BaseURL: endpoint + "/openai/v1",
			EnvKey:  "AIXGO_AZURE_OPENAI_API_KEY",
		}); err != nil {
			return nil, nil, fmt.Errorf("write codex config: %w", err)
		}
	}

	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}

	forge := &forgegh.Forge{StateLabels: app.StateLabels(prefix), Self: os.Getenv("AIXGO_GH_APP_LOGIN")}
	dry := *dryRun || os.Getenv("AIXGO_DRY_RUN") != ""
	if forge.Self == "" {
		if login, err := forge.Whoami(context.Background()); err == nil {
			forge.Self = login
		}
	}

	vcs := newVCS(forge.Self)
	vcs.DryRun = dry
	var forgeForLoop forge2.Forge = forge
	var dryForge *dryrun.Forge
	if dry {
		dryForge = dryrun.New(forge)
		forgeForLoop = dryForge
	}

	deps := app.Deps{
		Runner: newRunner(cfg, codexHome, *model),
		Forge:                  forgeForLoop,
		VCS:                    vcs,
		Worktrees:              &worktree.Manager{RepoDir: *repoDir, BaseDir: filepath.Join(*stateDir, "worktrees")},
		WorkflowRestrictedPush: isBotLogin(forge.Self),
		SelfLogin:              forge.Self,
	}
	return &commonFlags{repoDir: *repoDir, base: *base, actor: *actor, cfg: cfg, deps: deps, dry: dryForge}, rest, nil
}

func reportDryRun(c *commonFlags, w io.Writer) {
	if c == nil || c.dry == nil {
		return
	}
	report := c.dry.Report()
	fmt.Fprintln(w)
	fmt.Fprintln(w, report)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "## Aixgo Code dry run\n\n```\n%s\n```\n", report)
	}
}

func runCmd(argv []string) error {
	c, rest, err := prepare("run", argv)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("missing issue ref (want owner/repo#N)")
	}
	iss, err := app.ParseIssueRef(rest[0])
	if err != nil {
		return err
	}
	if err := app.Authorize(context.Background(), c.deps, iss.Repo, c.actor); err != nil {
		return err
	}
	if err := fetchIssue(&iss); err != nil {
		return fmt.Errorf("fetch issue: %w", err)
	}

	if c.dry != nil {
		fmt.Println("DRY RUN: no GitHub writes and no push will happen.")
	}
	fmt.Printf("running %s#%d against gate %q ...\n", iss.Repo, iss.Number, c.cfg.Gate)
	res, err := app.Run(context.Background(), c.deps, c.cfg, iss, c.base)
	defer reportDryRun(c, os.Stdout)
	if err != nil {
		return err
	}
	if res.Outcome == loop.OutcomePROpened {
		fmt.Printf("PR opened after %d round(s): %s\n", res.Rounds, res.PRURL)
	} else {
		fmt.Printf("blocked after %d round(s): %s\n", res.Rounds, res.Reason)
	}
	return nil
}

func addressCmd(argv []string) error {
	c, rest, err := prepare("address", argv)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("missing pull-request ref (want owner/repo#PR)")
	}
	ref, err := app.ParseIssueRef(rest[0])
	if err != nil {
		return err
	}
	if err := app.Authorize(context.Background(), c.deps, ref.Repo, c.actor); err != nil {
		return err
	}

	fmt.Printf("addressing feedback on %s#%d against gate %q ...\n", ref.Repo, ref.Number, c.cfg.Gate)
	res, err := app.AddressPR(context.Background(), c.deps, c.cfg, ref.Repo, ref.Number)
	defer reportDryRun(c, os.Stdout)
	if err != nil {
		return err
	}
	switch res.Outcome {
	case loop.OutcomeChangesPushed:
		fmt.Printf("changes pushed after %d round(s); re-requested review\n", res.Rounds)
	case loop.OutcomeNoFeedback:
		fmt.Println("no new review feedback to address; nothing to do")
	default:
		fmt.Printf("blocked after %d round(s): %s\n", res.Rounds, res.Reason)
	}
	return nil
}

func appCreateURL(repoDir string) (string, bool) {
	out, err := exec.Command("gh", "repo", "view", "--json", "owner", "--jq", ".owner.login").Output()
	_ = repoDir
	if err != nil {
		return "https://github.com/settings/apps/new", false
	}
	owner := strings.TrimSpace(string(out))
	if owner == "" {
		return "https://github.com/settings/apps/new", false
	}
	kind, err := exec.Command("gh", "api", "users/"+owner, "--jq", ".type").Output()
	if err != nil {
		return "https://github.com/settings/apps/new", false
	}
	if strings.TrimSpace(string(kind)) == "Organization" {
		return "https://github.com/organizations/" + owner + "/settings/apps/new", true
	}
	return "https://github.com/settings/apps/new", true
}

func workflowTemplateTag(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if isReleaseVersion(version) {
		return "v" + version
	}
	return latestKnownWorkflowTag
}

func isReleaseVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func fetchIssue(iss *domain.Issue) error {
	out, err := exec.Command("gh", "issue", "view", strconv.Itoa(iss.Number),
		"--repo", iss.Repo, "--json", "title,body").Output()
	if err != nil {
		return err
	}
	var v struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return err
	}
	iss.Title, iss.Body = v.Title, v.Body
	return nil
}
