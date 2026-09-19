# Changelog

All notable changes are recorded here and summarized again in the matching
GitHub release notes for each tag.

## Unreleased

## v0.6.0

- **Gemini on Vertex AI** (`engine: gemini`). The loop, roles, and gate are
  unchanged. The adapter shells out to the Gemini CLI (`gemini -p`) and reads
  `AIXGO_VERTEX_PROJECT`, `AIXGO_VERTEX_LOCATION` (default `us-central1`), and
  `AIXGO_VERTEX_API_KEY`. If that last value is a service-account JSON object
  (`"type": "service_account"`), it is written to a temp file and exported as
  `GOOGLE_APPLICATION_CREDENTIALS`; a plain API key is still passed as
  `GOOGLE_API_KEY`. Azure credentials are not required on this path.
- The reusable workflow accepts optional `vertex-project`, `vertex-location`,
  and `vertex-api-key`, and the Azure endpoint/key are no longer required
  inputs. A Codex adopter who already passes them is unchanged. A Gemini
  adopter omits Azure and passes the Vertex values instead. The workflow
  installs `@google/gemini-cli@0.60.0` when `vertex-project` is set.
- **`aixgo-code init` engine select.** Pass `--engine codex|gemini|claude`, or
  pick from a TTY menu. Writes `engine:` into `.github/aixgo.yml` and a
  conditional caller that wires only that provider's credentials. `--force`
  overwrites existing starters so you can switch engines. Non-interactive
  runs without `--engine` still default to `codex`.
- Onboarding docs (`README`, `docs/setup.md`) describe the engine-aware init
  flow and per-engine Variables/Secrets (org-level Actions config is fine).

## v0.5.0

**Breaking. The binary is `aixgo-code`, not `aixgo`.** The Aixgo framework's
own CLI is already named `aixgo` (`github.com/aixgo-dev/aixgo/cmd/aixgo`,
v0.7.x), and v0.4.0 took the same name, so `go install` of either tool silently
overwrote the other in `GOBIN`. Discovered the honest way: running
`aixgo version` after installing v0.4.0 and getting the framework's answer.
The coding agent yields the name; the framework had it first and is the
brand-named flagship.

Install with `go install github.com/aixgo-dev/code/cmd/aixgo-code@v0.5.0`, and
every CLI invocation gains the suffix: `aixgo-code init`, `aixgo-code
preflight`, `aixgo-code run`, `aixgo-code address`, `aixgo-code version`.

Nothing else changes. The config file is still `.github/aixgo.yml`, the
reusable workflow is still `aixgo.yml`, labels are still `ax:*`, the App
handle is still whatever `appName:` says, and all `AIXGO_*` variables and
secrets keep their names. A v0.4.0 install keeps working; it is just named
after the wrong tool on any machine that also uses the framework.

## v0.4.0

**Breaking. The project moved and was renamed.** The repository is now
`aixgo-dev/code` and the product is Aixgo Code. Nothing about how the agent
works changed in this release; every change below is a name.

Migrate each adopter repository in this order, because each step depends on the
one before it:

1. Rename `.github/simplycubed.yml` to `.github/aixgo.yml`, keeping its
   contents. Do the rename rather than running `init` on a repo with no config:
   `init` writes a blank starter, which would silently drop your `gate:`,
   `engine:`, `attribution:`, and everything else you had set. With the renamed
   file in place, re-running `init` keeps your values.
2. Run `aixgo init --workflow --app-name <your-app>` to write the new caller and
   self-test workflows under the new names.
3. Delete the old `.github/workflows/simplycubed.yml` caller. Nothing deletes it
   for you, and left in place it keeps firing on the same mention and label
   events: with the old variables still set you get duplicate runs for every
   command, and with the variables renamed you get a permanently failing run
   next to every real one.
4. Rename the repository variables and secrets and create the `ax:*` labels
   listed below.
5. Finish (or re-label and close out) any issue the agent has in flight before
   upgrading. The one-branch, one-pull-request, and one-state-label guards are
   keyed to the label prefix, so an issue mid-flight as `sc:working` on branch
   `sc/N` is invisible to a v0.4.0 run, which would start over as `ax/N` with a
   second pull request.

An unmigrated repo does not break; the new CLI reads `.github/aixgo.yml`, so it
behaves as if it has no config at all and refuses to run.

- **Module path.** `github.com/simplycubed/code` becomes
  `github.com/aixgo-dev/code`. Install with
  `go install github.com/aixgo-dev/code/cmd/aixgo@<release-tag>`.
- **Binary.** `simplycubed` becomes `aixgo`. Every subcommand is unchanged:
  `aixgo init`, `aixgo preflight`, `aixgo command`, `aixgo run`,
  `aixgo address`, `aixgo version`.
- **Config file.** `.github/simplycubed.yml` becomes `.github/aixgo.yml`.
- **Workflows.** The reusable workflow is
  `aixgo-dev/code/.github/workflows/aixgo.yml`, and `init --workflow` writes
  `.github/workflows/aixgo.yml` and `.github/workflows/aixgo-selftest.yml`.
- **Variables and secrets.** `SIMPLYCUBED_GH_APP_CLIENT_ID`,
  `SIMPLYCUBED_GH_APP_PRIVATE_KEY`, `SIMPLYCUBED_AZURE_OPENAI_ENDPOINT`,
  `SIMPLYCUBED_AZURE_OPENAI_API_KEY`, `SIMPLYCUBED_GH_APP_LOGIN`,
  `SIMPLYCUBED_DRY_RUN`, and `SIMPLYCUBED_SANDBOX` take the `AIXGO_` prefix. A
  value read by the old name is not read at all. For the required values that
  means a run with only the old names set fails on a missing value rather than
  using a stale one. The optional flags are the sharp edge: an exported
  `SIMPLYCUBED_DRY_RUN` or `SIMPLYCUBED_SANDBOX` is silently ignored, so a
  shell that relied on the old dry-run flag performs real writes until the
  export is renamed.
- **App name.** The App this repository installs is `aixgo-code`. Adopters keep
  their own App name; `appName:` was already per-adopter and is untouched by the
  rename.
- **Label prefix.** The default `labelPrefix` is `ax`, so the lifecycle is
  `ax:go`, `ax:queued`, `ax:working`, `ax:review`, `ax:blocked`, `ax:done`.
  Adopt the new labels rather than configuring the old ones back:
  `labelPrefix: sc` still works for CLI-driven runs, but the caller workflow
  `init` writes triggers on the literal `ax:go`, so an Actions-driven repo set
  to `sc` would apply `sc:go` and silently start nothing.
- **Scratch paths.** The per-run scratch directory is `.aixgo/` and the ledger
  branch is `aixgo/ledger`. The ledger starts fresh on the new branch; history
  from before the upgrade stays readable on the old `simplycubed/ledger`
  branch, which nothing writes to anymore.
- **Attribution marker.** Generated commits and pull requests are marked
  `Aixgo Code`.

Tags before `v0.4.0` remain in the repository's history, but their `go.mod`
still declares the old module path, so they are not installable as
`github.com/aixgo-dev/code`. To run a pre-move release, install it from
`github.com/simplycubed/code` at that tag.

## v0.3.0

**Comment commands address your own App.** v0.2.0 replaced `@simplycubed-code`
with `/simplycubed`, which fixed the multi-tenant bug and gave up the only thing
that makes a handle worth having. GitHub offers accounts **with repository
access** in its autocomplete, so a real mention completes after someone types
`@a` without them knowing the bot's name. A prefix that is not an account cannot
do that, and typing `/` opens GitHub's own menu, which is a fixed set of five
built-ins and matches nothing.

**Breaking.** `.github/simplycubed.yml` gains a required `appName:`,
`simplycubed init` requires `--app-name`, and the comment prefix changes again.
Re-run `simplycubed init --workflow --app-name <your-app>` to regenerate both
files.

- **`appName:` is the source of truth**, and the parser, help text,
  unknown-command reply, and wrong-surface replies all render from it. The agent
  can no longer tell anyone to mention a different account. It accepts
  `acme-code`, `@acme-code`, or `acme-code[bot]`, because all three are how
  people write the same App.
- **The workflow trigger stays a literal**, written by `init` from that same
  value. A workflow decides whether to start before any code runs, so matching
  loosely there would mint a token, check out, install Go, install the CLI and
  start a sandbox — roughly thirty seconds — every time someone mentioned a
  colleague, to then do nothing.
- **`preflight` fails when the two disagree.** The handle necessarily lives in
  two files and the App can push only one of them, since it holds no `workflows`
  permission by design. Drift is silent in the worst way: comments stop working
  with no error anywhere. The check names both values and says to re-run `init`.
- **`--app-name` is required at first install**, with an error that explains the
  name is yours rather than merely demanding a flag. Re-running `init` without
  it keeps whatever the config already says, so upgrading cannot silently change
  the handle a team already types.
- The install commands in the README and `docs/setup.md` show `<release-tag>`
  with a link to Releases, and `scripts/verify-release.sh` now also pins the
  README's "Current release" banner. v0.2.0 shipped with that banner still
  announcing v0.1.9, because it was prose rather than a checked pin.
- **`scripts/verify-release.sh` no longer breaks on formatting.** It matched the
  Go version pin as a fixed string, so gofmt realigning that constant made the
  check fail on the very release it was meant to guard. It now matches with a
  regex that tolerates the alignment.
- The README leads with what the product is. It opened with a dry run, ten lines
  before the overview, and carried three consecutive install sections that all
  pointed at `docs/setup.md`.

## v0.2.0

**This release makes the product installable by someone who is not us.** Every
change below comes from one root cause: it was built by the account that owns
the GitHub App, so the vendor case and the adopter case had never been
separated. Four of the defects were only reachable by an adopter, which is why
none of them had been seen.

**Breaking.** The command prefix and two configuration names change. There are
no known installations other than this repository, so no migration path is
provided; set the new names and regenerate the caller workflow.

- **You create your own GitHub App, and `init` now explains how.** The setup
  instructions told every adopter to create an App named `simplycubed-code`.
  App names are globally unique and we hold that one, so the step was only
  performable by the account that already owns it; everyone else got "Name has
  already been taken". Neither `init` nor the docs said why the App must be
  theirs: the private key is what mints tokens against their repository, so a
  shared key would let its holder act on every other installation. That single
  missing sentence is what made "create a GitHub App" read as busywork we could
  have done for them. `init` now prints the creation URL, resolved to the
  organisation form when the repository belongs to one, the three permissions
  with their access levels, webhook off and why, install visibility and why it
  is the step most often missed, and where the `Iv23` Client ID and the
  download-once private key come from.
- **Comment commands are `/simplycubed`, not `@simplycubed-code`.** An
  @-mention of our App cannot be right in anyone else's repository: their bot
  carries a different login, so the trigger matched nothing, and because our App
  is public the handle rendered there as a mention of an account they had never
  installed. The parser held the same literal, so templating the workflow
  trigger alone would not have helped. A prefix that is not a handle needs no
  templating and keeps one static caller workflow.
- **Workflow-push restriction is decided by identity, not by our App's name.**
  The check compared the authenticated login against a hardcoded
  `simplycubed-code[bot]`, so for every adopter it was false and the pre-flight
  never ran. They learned about the refusal at push time instead, after a full
  loop and its model spend. Any `[bot]` login is now restricted; an empty login
  stays unrestricted, because that is a human running locally and a human can
  push workflow files.
- **Escalations name the files that caused them.** "the change touches
  `.github/workflows/`" left a reader with nothing but the agent's own account
  of what it had edited, which is not evidence. Working out whether one real
  escalation was correct cost two investigation rounds and produced a wrong
  diagnosis. The message now lists the paths git actually reported.
- **The gate no longer stamps VCS metadata into binaries it throws away.**
  `go build` failed in the loop's worktree with `error obtaining VCS status:
  exit status 128` before any repository code compiled, so every agent run
  reported a red gate for a reason unrelated to the change under test. A gate
  that cannot run where the loop runs is not a gate.
- **One naming scheme for the four values an adopter sets.** Two carried the
  `SIMPLYCUBED_` prefix and two did not, so on an organisation settings page the
  four did not read as one tool's configuration and nobody auditing later could
  tell what `AZURE_OPENAI_API_KEY` belonged to. The unprefixed pair was also the
  conventional name for that credential, so an organisation already using Azure
  OpenAI either hit a conflict or silently shared one key between this runtime
  and something unrelated.

  | Section | Name |
  | --- | --- |
  | Variables | `SIMPLYCUBED_GH_APP_CLIENT_ID` |
  | Secrets | `SIMPLYCUBED_GH_APP_PRIVATE_KEY` |
  | Variables | `SIMPLYCUBED_AZURE_OPENAI_ENDPOINT` (was `AZURE_OPENAI_ENDPOINT`) |
  | Secrets | `SIMPLYCUBED_AZURE_OPENAI_API_KEY` (was `AZURE_OPENAI_API_KEY`) |

  `SIMPLYCUBED_SELF_LOGIN` becomes `SIMPLYCUBED_GH_APP_LOGIN`; it parsed as
  "SimplyCubed's login" when it means the bot identity the run holds.
- **`preflight` checks those four and names the section a missing one belongs
  in.** Nothing checked them until a real run failed, and the one thing that did
  was deleted at the end of install: the self-test names the section, then
  `init` tells the adopter to remove it. Variables and Secrets are different
  tabs, and a value filed under the wrong one reads back as empty rather than
  failing, which happened during a real install and was found by reading the
  caller workflow. A configuration miss now exits `3`, so a caller can tell "go
  and set this" from a bug without matching on message text.
- **Removed the `github-app-id` workflow input.** It survived as a fallback for
  callers that do not exist, and it invited the wrong credential: the name says
  App *ID*, the numeric one, while the token action needs the `Iv23` client ID.

## v0.1.9

- **The GitHub Actions runtime does the work now.** Until this release the
  issue-to-PR loop had never once produced a pull request from Actions, on this
  repository or any other. The engine confines itself with a bundled bubblewrap,
  bubblewrap builds its sandbox from an unprivileged user namespace, and Ubuntu
  24.04 forbids those by AppArmor policy, so the sandbox died at startup on
  every run:

  ```
  bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted
  ```

  Every command the engine tried then failed, including `pwd`, so it read
  nothing and changed nothing while still exiting successfully. That is why this
  presented for so long as an agent quietly declining to work rather than one
  that had never been able to start. The workflow now enables the kernel feature
  the sandbox is built from, which reads like a weakening and is the reverse:
  with the restriction in place the engine was confined by nothing, because its
  confinement never loaded. Measured on a runner in both directions, same
  binary, only that setting changing. None of the engines' own "dangerous" flags
  are set, here or anywhere.
- Comment commands answer on the thread instead of in a log nobody opens.
  `@simplycubed-code help` produced the right text and printed it inside the
  runner, so the person who asked saw silence. Worse, `address` on an issue ran
  anyway and failed two layers down with a raw GraphQL error, because nothing
  checked that a pull-request verb had been aimed at a pull request. The agent
  now resolves which surface a comment arrived on, answers with the verb that
  does apply, and stays green: someone using the wrong word is not a failure. A
  comment that merely mentions the agent in passing still says nothing at all.
- `engine: claude` no longer demands Azure credentials it never uses. The
  adapter was selectable and unusable, because the CLI required an Azure
  endpoint and key whatever engine was chosen, and then wrote a Codex provider
  config the Claude path never reads. Both are now conditional on the engine.
  This is the local CLI path; the reusable workflow still installs only the
  Codex CLI, which is tracked separately.
- The repository's own comment job installs the commit under test rather than
  the last release, matching the two jobs beside it. It was the one path whose
  regressions could not be caught before they shipped.

## v0.1.8

- **The Actions runtime was dead in `v0.1.7` and is fixed here.** The comment
  dispatch fix gave the run step a second `env:` block instead of adding to the
  first. YAML keeps the last of two identical keys, so `GH_TOKEN` and both Azure
  variables were dropped, and GitHub, which is stricter than the parser the gate
  used, refused to load the file at all. Every run since that merge failed in
  about a second without starting a job. Upgrade from `v0.1.7`.
- The gate now rejects a duplicate YAML key anywhere in a workflow, template, or
  the embedded caller. `yaml.safe_load` accepted the broken file and kept the
  last key, which is exactly why the break reached a release: green gate, dead
  workflow. This is the second time a workflow defect passed a gate that only
  compiled Go, and the check is written to fail on the real file from `v0.1.7`.
- The App token step now uses `client-id`, which ends the deprecation warning
  on every run. The two inputs are not interchangeable in principle, but the
  action resolves `client-id || app-id` into one value, so adopters passing
  `github-app-id` keep working while the documented path moves to
  `SIMPLYCUBED_GH_APP_CLIENT_ID`.

## v0.1.7

- Fixed comment commands. The reusable workflow declared `comment-body` and never
  read it, so nothing called the parser: any comment beginning with the mention,
  from anyone with write access, started a full run. `@simplycubed-code help`
  started work instead of printing help, and a comment asking it not to proceed
  started the work anyway.
- Documentation caught up with the code. The README claimed the reviewer was not
  wired in and that the Claude adapter was planned, both shipped; `STATUS.md`
  named the wrong release and listed SHA-pinned actions as not done after they
  shipped. `preflight`, `--dry-run`, and the install self-test had no
  adopter-facing documentation at all and now do.
- New `docs/troubleshooting.md`, starting from the symptom every install-time
  failure shares: the run went green and nothing happened.
- Three diagrams: the credential split between the local CLI and Actions, the
  diagnosis tree for a silent success, and what triggers a run.

## v0.1.6

- **The Actions runtime works end to end.** Two environment blockers are fixed:
  the engine's sandbox could not start inside a runner, so the agent could not
  execute a single command; and a runner has no git identity, so commits failed
  after the gate had already passed.
- **Automated reviewer** (`review: true`, off by default). After the gate
  passes, a read-only reviewer judges the change and its findings go to the
  fixer before a human sees the pull request. It never approves and never
  merges. A pass carrying a blocker is not trusted.
- **Claude Code engine** (`engine: claude`, default `codex`). The loop, roles,
  and gate are unchanged by which model writes the code.
- **Comment commands**: `@simplycubed-code go`, `address`, and `help`, acted on
  only for commenters with write access.
- The reusable workflow carries values rather than decisions: authorization,
  identity resolution, and engine validation moved into the product, where they
  are tested. It is 265 lines shorter and its two jobs are now identical.
- Releases are cut by a workflow, not a local tag, and refuse to proceed when
  the changelog or any version pin disagrees with the version being released.
- Actions are pinned by commit SHA, with Dependabot keeping them current.
- The run ledger persists to an orphan branch, so a run's audit trail outlives
  the runner it happened on.
- An escalation now carries the engine's own account of the turn, which is what
  made the two blockers above diagnosable at all.

## v0.1.5

Supersedes v0.1.4, which was tagged from the wrong commit and is retracted in
`go.mod`. The content below is what v0.1.4 was meant to carry.

- Fixed the blocker that made every GitHub Actions run fail before doing any
  work: the CLI defaulted `--base` to `origin/HEAD`, which `actions/checkout`
  never creates. The worktree manager now resolves the remote's default branch,
  and the reusable workflow passes the base from the triggering event.

## v0.1.3

Supersedes v0.1.2, which was tagged from the wrong commit and is retracted in
`go.mod`. The content below is what v0.1.2 was meant to carry.

**Breaking (GitHub Actions runtime only):** the reusable workflow now
authenticates as the `simplycubed-code` GitHub App and no longer accepts the
`gh-token` personal-access-token secret. Callers moving from `v0.1.1` must
create and install the App, then supply `github-app-id` and
`github-app-private-key`. Callers pinned at `v0.1.1` are unaffected until they
bump. The local CLI path is unchanged and needs no App.

- GitHub App identity: each job mints its own installation token scoped to the
  current repository with contents, issues, and pull-requests permissions only,
  and verifies in the run log that it cannot reach Actions administration. The
  agent authors commits, pull requests, and comments as `simplycubed-code[bot]`,
  and its pull requests receive their own CI runs (which a `GITHUB_TOKEN`-
  authored pull request never does).
- ADR 0006's open one-App-versus-two question is decided: one App carries every
  role, with per-job token scoping for least privilege.
- Adopter docs rewritten around the shipped install path, including a
  local-CLI-first quickstart, and corrected to state plainly that the runtime
  holds no `workflows` permission and cannot add its own workflow files.
- Coverage reporting: the gate now runs tests with `-race` and writes a
  coverage profile, uploaded to Codecov from CI.

## v0.1.1

- Reusable GitHub Actions workflow: run the issue-to-PR and fix-on-request
  loops inside an adopter's own Actions, with labeler/reviewer authorization,
  per-item concurrency, a self-review skip, and a configurable engine model.
- `simplycubed init --workflow` writes the version-pinned Actions caller
  workflow, so installing into a repo is one merged PR plus one secret.
- Release-on-tag workflow: pushing a `v*` tag re-runs the gate, enforces the
  CHANGELOG and stamped-version checks, and publishes the GitHub release.
- Generated pull-request descriptions (`prDescription: rich`): walkthrough,
  changes table, and sequence diagram on PRs the agent opens.
- CLI hardening: flags are honored before or after positional arguments, and
  engine scratch (`.gopath` module cache) can no longer leak into commits.

## v0.1.0

- First tagged release.
- Added runtime version stamping so `go install ...@v0.1.0` reports `0.1.0` in
  `simplycubed version`.
- Documented the pinned install form and the minimal release process.
