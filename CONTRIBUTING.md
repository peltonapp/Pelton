<img width="2560" height="1440" alt="contr-pelton" src="https://github.com/user-attachments/assets/e18a6bd6-ecbb-4b62-9585-09b55321dab0" />

<p align="center"><strong>First of all, thanks for taking the time to contribute! 🎉</strong></p>

*Before Contributing please read the Information below.*

**There are multiple ways you can Contribute to the Pelton Project:**
1. [Submit a Theme ↗](https://github.com/peltonapp/themes/issues/new?template=submit_theme.yml)
2. [Contribute to the Website (pelton.app) ↗](https://github.com/peltonapp/website)
3. [Submitting a Feature (Request)](https://github.com/peltonapp/Pelton/issues)
4. [Reporting a Bug](https://github.com/peltonapp/Pelton/issues)
5. [Requesting a Language or translating Pelton](https://github.com/peltonapp/Pelton/issues)
6. Submitting PRs
   
## Code of Conduct

The rules are short because they have to be remembered, not looked up. Be respectful. Don't be rude to people asking questions, filing their first issue, or making mistakes. If you'd be embarrassed saying it face-to-face, don't type it.

Assume good faith. When something someone wrote could be read two ways, pick the charitable reading. Non-native English speakers sometimes sound blunt when they're being neutral. New contributors miss things because they haven't read the docs yet, not because they're disrespecting you. Ask before you assume. Disagree on ideas, not people. "This approach has a race condition" is fine. "You clearly don't understand concurrency" is not. Code can be wrong. People shouldn't be attacked for writing it.

Zero tolerance for harassment or discrimination. Targeting someone for their race, gender, sexuality, religion, nationality, or disability, threatening, or doxxing anyone gets you banned from the project. No warnings, no debate.

Report problems to [me@arne.sh ↗](mailto:me@arne.sh). Reports are handled privately.

*TLDR; Be nice and responsible*

## AI-Assisted Development

Pelton has files to guide AI, starting with [AGENTS.md](https://github.com/peltonapp/Pelton/blob/main/AGENTS.md) as the entrypoint.

**Allowance**

You may use AI tools such as Claude Code, Codex, Aider, or similar. Fully autonomous systems that open PRs without human review are not allowed.

**Limits**

AI can be used for writing code, but architecture and the way things should work, look, and be implemented has to come from a human.

When working on the frontend, stick to Pelton's existing design — don't let AI introduce generic patterns like purple-blue gradients.

**Disclosure**

Contributors must state AI usage in the commit or PR description, e.g. `Assisted-by: Claude Code`. This is for transparency only and doesn't lead to different handling of the contribution. It's fully okay. 

*TLDR; Yes.*

## Developer Certificate of Origin (DCO)

### Full DCO text

The full, unmodified text of the Developer Certificate of Origin 1.1 is in [DCO.md](./DCO.md). When you sign off a commit, that's what you're attesting to.

### Why

Two things matter when you send code to Pelton:

1. You actually wrote it, or you have the right to submit it under the project's GPL-3.0 license. Code copied from a GPL project, code owned by your employer, or AI-generated code can't be accepted unless the permissions check out.
2. You're fine with it being GPL-3.0 from that point on. Once it's merged it stays under that license.

The DCO is how you confirm both, in one line, per commit.

### How to sign off

Set your name and email in git, once:

```bash
git config --global user.name "John S."
git config --global user.email "john@example.com"
```

Then pass `-s` when you commit:

```bash
git commit -s -m "fix: handle nil response in HTTP check"
```

That adds a line at the bottom of your commit message:

```text
Signed-off-by: John S. <john@example.com>
```

The DCO bot checks every PR. If a commit is missing the sign-off, it blocks the merge until you fix it.

> [!TIP]
> If using Claude Code or Codex, use a prompt like `Add -s to git commits.`.


## Commit Message Convention

Pelton uses [Conventional Commits](https://www.conventionalcommits.org/) for commit messages. This keeps the history readable and allows changelogs to be generated automatically.

Each commit message should follow this format:

```text
<type>(<optional scope>): <short description>

<optional body>

<optional footer(s)>
```

**Common types:**
- `feat` — a new feature
- `fix` — a bug fix
- `docs` — documentation-only changes
- `style` — formatting, missing semicolons, etc. (no code logic change)
- `refactor` — code change that neither fixes a bug nor adds a feature
- `perf` — performance improvement
- `test` — adding or correcting tests
- `chore` — build process, tooling, dependency updates

**Examples:**
```text
feat(oauth): add Google OAuth token refresh
fix(inbox): resolve crash when marking empty selection as read
docs: update setup instructions in README
```

Breaking changes should be indicated with a `!` after the type/scope, and explained in the footer:
```text
feat(api)!: change account sync response format

BREAKING CHANGE: sync endpoint now returns paginated results
```

Please keep the short description under ~72 characters, written in the imperative mood ("add" not "added" or "adds"). Don't forget to sign off your commits per the DCO requirement above.

## Pull Requests and Reviews

Pelton is maintained by one person in their free time. I go through new issues and pull requests about once a week, so a reply can take a few days. If nobody has answered after two weeks, a friendly ping on the issue or PR is welcome.

**Open an issue first for features and large changes.** Bug fixes and small improvements can go straight to a PR. For a new feature, a change in how something behaves, or a PR of more than about 500 changed lines, please open an issue (or comment on an existing one) and wait until we have agreed on the approach. That way nobody spends days on something that doesn't fit Pelton or conflicts with other work.

**Keep pull requests small.** One change per PR. Large work is easier to review, and to land, as a series of small PRs that each make sense on their own than as one big one.

**First PRs need a click from me.** For first-time contributors, GitHub waits for a maintainer to approve the CI run. That's normal and doesn't mean anything is wrong with your PR.

*TLDR; Issue first for big things, small PRs, expect about a week.*

## Triage

Some regular contributors have Triage access. Triage can label, sort and assign issues and PRs, close duplicates and ask reporters for missing details. It cannot push or merge.

If you have Triage access:
- Label and sort freely, and close duplicates with a link to the original.
- Ask reporters for logs, versions or steps when a report is missing them.
- Leave milestones, closing feature requests, and any decision about what Pelton should or shouldn't do to the maintainer. When unsure, leave a comment and let me decide.

## Branches

**Send every pull request to `main`.** That is the only branch you need to think about, whether you're fixing a typo or adding a feature.

Branch off `main`, name it `type/short-slug` to match your commit type, and keep unrelated changes out of it. One branch per change.

You'll also see `release/<version>` branches, such as `release/2026.4`. Those are the versions people have actually installed, and they only ever receive fixes that are already on `main`, copied over by a maintainer when something needs to ship before the next release. Please don't branch from them or open PRs against them.

`main` is frequently in a half-finished state between releases, which is expected. If something on `main` looks broken, it may simply not be finished yet, so check the open PRs and issues before reporting it.

## Getting credited

It's appreciated if you add yourself to [AUTHORS.md ↗](https://github.com/peltonapp/Pelton/blob/main/AUTHORS.md) so you get credited by name. Add yourself in the same PR, or in a follow-up update it.

## Pelton-specific

As Pelton is privacy focused you have a couple more rules:
- No telemetry or phone-home (self-explanatory)
- Any external request (except for IMAP/SMTP) must be off by default
  > If you add anything that even reaches out to any 3rd party server, it has to be off by default.
  > 
  > **Example:** Let's say you'd be adding a VirusTotal integration: Off by default and must be enabled first in settings.


- Most things should be customizable and toggleable in settings. Make sure to put the setting you're adding in a fitting place.
