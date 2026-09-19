package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aixgo-dev/code/internal/app"
	"github.com/aixgo-dev/code/internal/buildinfo"
	"github.com/aixgo-dev/code/internal/config"
	forgegh "github.com/aixgo-dev/code/internal/forge/gh"
)

const (
	callerWorkflowEngineToken  = "__AIXGO_ENGINE__"
	callerWorkflowInputsToken  = "# __AIXGO_ENGINE_INPUTS__"
	callerWorkflowSecretsToken = "# __AIXGO_ENGINE_SECRETS__"
	defaultInitEngine          = "codex"
)

// initStdinIsTTY reports whether stdin is an interactive terminal. Tests
// override it so non-TTY defaulting and the menu path are both reachable.
var initStdinIsTTY = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// initPromptIn is the reader for the interactive engine menu. Tests swap it.
var initPromptIn io.Reader = os.Stdin

func initCmd(argv []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	writeWorkflow := fs.Bool("workflow", false, "also write the Actions caller workflow")
	appNameFlag := fs.String("app-name", "", "the GitHub App you installed, for example acme-code; comment commands address it")
	engineFlag := fs.String("engine", "", "model engine: codex (Azure), gemini (Vertex), or claude")
	force := fs.Bool("force", false, "overwrite existing starter config and workflow files")
	if _, err := parseInterleaved(fs, argv); err != nil {
		return err
	}

	engine, err := resolveEngine(*engineFlag, initStdinIsTTY(), initPromptIn, stdout)
	if err != nil {
		return err
	}

	appName, err := resolveAppName(*repoDir, *appNameFlag)
	if err != nil {
		return err
	}

	configPath := filepath.Join(*repoDir, ".github", "aixgo.yml")
	wroteConfig, err := writeStarterConfig(configPath, appName, engine, *force)
	if err != nil {
		return err
	}
	workflowPath := filepath.Join(*repoDir, ".github", "workflows", "aixgo.yml")
	selftestPath := filepath.Join(*repoDir, ".github", "workflows", "aixgo-selftest.yml")
	wroteWorkflow := false
	wroteSelftest := false
	if *writeWorkflow {
		wroteWorkflow, err = writeStarterFile(workflowPath, renderCallerWorkflow(buildinfo.Version, appName, engine), *force)
		if err != nil {
			return err
		}
		wroteSelftest, err = writeStarterFile(selftestPath, selftestWorkflowTemplate, *force)
		if err != nil {
			return err
		}
	}

	labels := app.StateLabels(config.DefaultLabelPrefix)
	created, err := (&forgegh.Forge{Dir: *repoDir}).EnsureLabels(context.Background(), labels)
	if err != nil {
		return err
	}

	if wroteConfig {
		fmt.Fprintf(stdout, "wrote %s\n", configPath)
	} else {
		fmt.Fprintf(stdout, "left existing %s unchanged\n", configPath)
	}
	if *writeWorkflow {
		if wroteWorkflow {
			fmt.Fprintf(stdout, "wrote %s\n", workflowPath)
		} else {
			fmt.Fprintf(stdout, "left existing %s unchanged\n", workflowPath)
		}
		if wroteSelftest {
			fmt.Fprintf(stdout, "wrote %s\n", selftestPath)
		} else {
			fmt.Fprintf(stdout, "left existing %s unchanged\n", selftestPath)
		}
	}
	if len(created) == 0 {
		fmt.Fprintln(stdout, "labels already present: no changes")
	} else {
		fmt.Fprintf(stdout, "created labels: %s\n", strings.Join(created, ", "))
	}
	printInitNextSteps(stdout, *repoDir, engine)
	return nil
}

// resolveEngine picks the model engine for starter files. An explicit --engine
// wins; otherwise a TTY gets a short menu; otherwise codex is the default so
// existing non-interactive scripts keep working.
func resolveEngine(flagValue string, interactive bool, in io.Reader, out io.Writer) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		return normalizeEngine(flagValue)
	}
	if interactive {
		return promptEngine(in, out)
	}
	fmt.Fprintf(out, "using engine: %s (default; pass --engine or run interactively to choose)\n", defaultInitEngine)
	return defaultInitEngine, nil
}

func normalizeEngine(v string) (string, error) {
	switch e := strings.ToLower(strings.TrimSpace(v)); e {
	case "codex", "claude", "gemini":
		return e, nil
	default:
		return "", fmt.Errorf("--engine must be codex, claude, or gemini, got %q", v)
	}
}

func promptEngine(in io.Reader, out io.Writer) (string, error) {
	fmt.Fprintln(out, "Select the model engine:")
	fmt.Fprintln(out, "  1) Codex / Azure OpenAI (default)")
	fmt.Fprintln(out, "  2) Gemini / Vertex AI")
	fmt.Fprintln(out, "  3) Claude (local CLI; Actions support pending)")
	fmt.Fprint(out, "Enter choice [1]: ")
	buf := make([]byte, 0, 16)
	tmp := make([]byte, 1)
	for {
		n, err := in.Read(tmp)
		if n > 0 {
			if tmp[0] == '\n' {
				break
			}
			if tmp[0] != '\r' {
				buf = append(buf, tmp[0])
			}
		}
		if err != nil {
			if len(buf) == 0 && err == io.EOF {
				break
			}
			if err != io.EOF {
				return "", err
			}
			break
		}
	}
	choice := strings.TrimSpace(string(buf))
	switch choice {
	case "", "1":
		return "codex", nil
	case "2":
		return "gemini", nil
	case "3":
		return "claude", nil
	default:
		if e, err := normalizeEngine(choice); err == nil {
			return e, nil
		}
		return "", fmt.Errorf("unknown engine choice %q (use 1, 2, 3, or codex|gemini|claude)", choice)
	}
}

func printInitNextSteps(stdout io.Writer, repoDir, engine string) {
	createURL, resolved := appCreateURL(repoDir)
	fmt.Fprintln(stdout, "next steps:")
	fmt.Fprintln(stdout, "  1. Create your OWN GitHub App. It has to be yours: its private key is what mints")
	fmt.Fprintln(stdout, "     the tokens that act on your repository, so a shared key would let its holder act")
	fmt.Fprintln(stdout, "     on every other installation. This is what keeps the agent inside your GitHub.")
	fmt.Fprintln(stdout, "       "+createURL)
	if !resolved {
		fmt.Fprintln(stdout, "       (if this repository belongs to an organisation, use")
		fmt.Fprintln(stdout, "        https://github.com/organizations/<org>/settings/apps/new instead)")
	}
	fmt.Fprintln(stdout, "     Name it anything you like; App names are globally unique, so you cannot reuse ours.")
	fmt.Fprintln(stdout, "     Permissions, and nothing else:  Contents: Read and write")
	fmt.Fprintln(stdout, "                                     Issues: Read and write")
	fmt.Fprintln(stdout, "                                     Pull requests: Read and write")
	fmt.Fprintln(stdout, "     Uncheck Active under Webhook. Actions triggers this runtime; an enabled webhook")
	fmt.Fprintln(stdout, "     with nothing listening only generates failures.")
	fmt.Fprintln(stdout, "     Set Any account under Where can this GitHub App be installed, if the App belongs")
	fmt.Fprintln(stdout, "     to your personal account and the repository belongs to an organisation. An App")
	fmt.Fprintln(stdout, "     restricted to its owner cannot be installed anywhere else, and this is the step")
	fmt.Fprintln(stdout, "     most often missed.")
	fmt.Fprintln(stdout, "  2. Generate a private key on the App settings page and keep the download. GitHub")
	fmt.Fprintln(stdout, "     shows it once. Note the Client ID there too, the Iv23 string.")
	fmt.Fprintln(stdout, "  3. Install the App on this repository, from Install App on the same page.")
	fmt.Fprintln(stdout, "     Reference: https://docs.github.com/apps/creating-github-apps")
	fmt.Fprintln(stdout, "  - write the real gate in .github/aixgo.yml")
	fmt.Fprintln(stdout, "  - verify that gate is green on your main branch")
	printInitCredentialSteps(stdout, engine)
	fmt.Fprintln(stdout, "    Variables and Secrets are different tabs. A value filed under the wrong one reads back")
	fmt.Fprintln(stdout, "    as empty, and the run fails without saying why.")
	fmt.Fprintln(stdout, "  - merge the PR containing the config and workflow changes")
	fmt.Fprintln(stdout, "  - run the self-test once: gh workflow run aixgo-selftest")
	fmt.Fprintln(stdout, "  - delete .github/workflows/aixgo-selftest.yml once it passes")
	fmt.Fprintln(stdout, "  - file an issue and apply the ax:go label")
	fmt.Fprintln(stdout, "  Existing starter files are left unchanged; pass --force to overwrite them")
	fmt.Fprintln(stdout, "  (for example to switch engines with --engine).")
}

func printInitCredentialSteps(stdout io.Writer, engine string) {
	fmt.Fprintln(stdout, "  - add repository VARIABLES, under Settings > Secrets and variables > Actions > Variables:")
	fmt.Fprintln(stdout, "      AIXGO_GH_APP_CLIENT_ID       the App Client ID, the Iv23 string on the App settings page")
	switch engine {
	case "gemini":
		fmt.Fprintln(stdout, "      AIXGO_VERTEX_PROJECT         Google Cloud project for Vertex AI")
		fmt.Fprintln(stdout, "      AIXGO_VERTEX_LOCATION        optional; default us-central1")
		fmt.Fprintln(stdout, "  - add repository SECRETS, on the Secrets tab of that same page:")
		fmt.Fprintln(stdout, "      AIXGO_GH_APP_PRIVATE_KEY     the full PEM, including the BEGIN and END lines")
		fmt.Fprintln(stdout, "      AIXGO_VERTEX_API_KEY         Vertex API key or service-account JSON contents")
	case "claude":
		fmt.Fprintln(stdout, "  - add repository SECRETS, on the Secrets tab of that same page:")
		fmt.Fprintln(stdout, "      AIXGO_GH_APP_PRIVATE_KEY     the full PEM, including the BEGIN and END lines")
		fmt.Fprintln(stdout, "  - Claude locally: use your existing `claude` CLI login; no Azure or Vertex vars.")
		fmt.Fprintln(stdout, "  - Claude on Actions is not wired yet (aixgo-dev/code#147). The caller has no")
		fmt.Fprintln(stdout, "    Anthropic secret input until that lands; prefer local `aixgo-code` for Claude today.")
	default:
		fmt.Fprintln(stdout, "      AIXGO_AZURE_OPENAI_ENDPOINT  e.g. https://<resource>.openai.azure.com")
		fmt.Fprintln(stdout, "  - add repository SECRETS, on the Secrets tab of that same page:")
		fmt.Fprintln(stdout, "      AIXGO_GH_APP_PRIVATE_KEY     the full PEM, including the BEGIN and END lines")
		fmt.Fprintln(stdout, "      AIXGO_AZURE_OPENAI_API_KEY   the Azure OpenAI key")
	}
}

func writeStarterConfig(path, appName, engine string, force bool) (bool, error) {
	body := strings.ReplaceAll(starterConfig, callerWorkflowAppNameToken, appName)
	body = strings.ReplaceAll(body, callerWorkflowEngineToken, engine)
	return writeStarterFile(path, body, force)
}

// resolveAppName decides the handle to write into both files. An explicit
// --app-name wins; otherwise an existing config keeps what it already says, so
// re-running init to pick up a new release does not silently change the handle
// a team already types.
func resolveAppName(repoDir, flagValue string) (string, error) {
	if v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(flagValue), "@"), "[bot]"); v != "" {
		return v, nil
	}
	if cfg, err := config.Load(filepath.Join(repoDir, ".github", "aixgo.yml")); err == nil && cfg.AppName != "" {
		return cfg.AppName, nil
	}
	return "", errors.New("--app-name is required: comment commands address the App you installed, and its name is unique to you. Pass the App's name without the \"[bot]\" suffix, for example --app-name acme-code")
}

func writeStarterFile(path, body string, force bool) (bool, error) {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// renderCallerWorkflow writes the trigger for the App this repository installed
// and only the credentials for the chosen engine. The handle cannot be a
// constant: App names are globally unique, so every adopter's bot has its own,
// and the trigger has to match theirs or no comment ever starts a run. It is
// templated here rather than matched loosely at runtime so a mention of a
// colleague does not spin up a runner.
func renderCallerWorkflow(version, appName, engine string) string {
	inputs, secrets := callerEngineBlocks(engine)
	out := strings.ReplaceAll(callerWorkflowTemplate, callerWorkflowTagToken, workflowTemplateTag(version))
	out = strings.ReplaceAll(out, callerWorkflowAppNameToken, appName)
	out = strings.ReplaceAll(out, callerWorkflowInputsToken, inputs)
	out = strings.ReplaceAll(out, callerWorkflowSecretsToken, secrets)
	return out
}

func callerEngineBlocks(engine string) (inputs, secrets string) {
	switch engine {
	case "gemini":
		inputs = "      vertex-project: ${{ vars.AIXGO_VERTEX_PROJECT }}\n" +
			"      vertex-location: ${{ vars.AIXGO_VERTEX_LOCATION }}\n" +
			"      # model: gemini-3.5-flash"
		secrets = "      vertex-api-key: ${{ secrets.AIXGO_VERTEX_API_KEY }}\n" +
			"      github-app-private-key: ${{ secrets.AIXGO_GH_APP_PRIVATE_KEY }}"
	case "claude":
		inputs = "      # Claude on Actions is not wired yet (aixgo-dev/code#147):\n" +
			"      # the reusable workflow does not install the Claude CLI or accept\n" +
			"      # an Anthropic secret. Local `aixgo-code` with engine: claude uses\n" +
			"      # your existing claude CLI login."
		secrets = "      github-app-private-key: ${{ secrets.AIXGO_GH_APP_PRIVATE_KEY }}"
	default:
		inputs = "      azure-openai-endpoint: ${{ vars.AIXGO_AZURE_OPENAI_ENDPOINT }}\n" +
			"      # model: my-gpt-5-4-deployment"
		secrets = "      azure-openai-api-key: ${{ secrets.AIXGO_AZURE_OPENAI_API_KEY }}\n" +
			"      github-app-private-key: ${{ secrets.AIXGO_GH_APP_PRIVATE_KEY }}"
	}
	return inputs, secrets
}
