<!-- markdownlint-disable MD007 -- Unordered list indentation -->
<!-- markdownlint-disable MD010 -- hard tabs -->
<!-- markdownlint-disable MD033 -- inline html -->
<!-- markdownlint-disable MD055 -- Table pipe style [Expected: leading_and_trailing; Actual: leading_only; Missing trailing pipe] -->
<!-- markdownlint-disable MD041 -- First line in a file should be a top-level heading -->

# Style guide

Code here takes the boring, obvious way over the clever one. Where a formatter or linter has an opinion, it wins.

## Go

- gofmt formats everything. go vet, staticcheck, and gosec run clean in the gate.

- Errors go back to the caller at once, wrapped with `%w` when the caller can use more context. An error that's dropped on purpose gets a short comment saying why.

- The happy path stays at the left margin. Return early instead of nesting.

- An interface is defined where it's used, and only once there are 2 implementations or a test needs a stand-in.

- A package-level `var` that tests replace says so in its comment. That's how tests stay off the host's real sudo, accounts, and network.

- Root never follows a link inside the server tree, since the server account can write there. Writes go through folder handles opened with `O_NOFOLLOW`, as `source/internal/settings` does.

- Text that the server or a plugin wrote goes through `server.Clean` before it reaches the terminal.

## Tests

- Every test starts with `defer testid.Line(t, "<id>")`. A new ID is the minutes since 2000-01-01 UTC in Crockford base 32, plus a 2-character counter, from `cicd/utility/test-id.bash --new`. `--check` refuses one that's missing, malformed, or used twice.

- Unit tests stay off the real host. They use `host.Fake` for commands and stand-in files for anything under `/etc`.

## Bash

- Files end in `.bash`. The top has the shebang, then any `shellcheck disable` lines with a reason each, then a `##` header with the purpose, copyright, and license. History goes at the bottom.

- Functions are `fCamelCase`, and variables are camelCase. Every variable a function owns is `local`.

- `[[ ]]`, never `[ ]`. Every expansion is braced and quoted, as in `"${var}"`.

- shellcheck runs clean. No formatter is used.

- A loop doesn't start a command or a subshell for each item when a builtin, or one pass of a tool, does the job.

## Comments

- Short, and about why. A comment that says what the next line does gets cut.

- A long reason goes in `project/design.md`, and the comment names the section.

- Deferred code stays, commented out with a one-line reason, until it's picked up or canceled.
