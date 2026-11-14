# monorepo

A monorepo for the harness: one framework, in `framework/`, and two
projects on it, `projects/harbor` and `projects/slipway`. `harness.yaml` at
the root names them, and says who can give the bots work, which agents they
may be, and the server they run on.

- `harness test` tests what the folder you are in belongs to: a project, or,
  anywhere else, the framework and every project. `-p <project>`, or
  `-p framework`, picks one instead. The arguments after it are the framework's:
  see `framework/README.md`.
- The framework changes with its projects: a breaking change to it and the
  changes to the projects land in the same pull request.
- Each project keeps its own rules in its `AGENTS.md` or `CLAUDE.md`; read
  them before changing it.
- Every change reaches `main` through a squash-merged pull request whose
  title is a conventional commit (`feat:`, `fix:`, `docs:`, `chore:`…, `!`
  for a breaking change). Each project is released on its own, tagged
  `<project>/v<version>`, from the titles of the pull requests that changed
  its folder.
- `harness.yaml` and `.github/` decide what the bots may do. Change them
  only when asked.
