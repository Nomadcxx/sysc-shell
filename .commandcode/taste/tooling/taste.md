# Tooling Preferences

- Never run `go test -race`, `go test ./...`, `go tool trace`, or `GODEBUG=allocfreetrace` — they OOM this machine. Use focused, scoped test commands instead (e.g. `timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run '<Name>'`). Confidence: 0.97
- gopls reports false errors inside git worktrees; treat `go build` and `go vet` as the authority, not the LSP. Confidence: 0.6
- Go-first project (a Wayland/Niri desktop shell); run `gofmt -w` on touched files and `go vet` on touched packages only, not `./...`. Confidence: 0.75
- Values minimal dependencies and standard-library solutions over adding frameworks/abstractions. Confidence: 0.7
- The `gh` CLI is not usable in this environment — calls hang then fail with HTTP 401 even though `git push` works via stored credentials. When a PR is needed, kill the hung `gh` task and create it through the GitHub API using `git credential fill` for the token, never printing or echoing the token. Confidence: 0.6
- The session shell's default cwd can silently snap back to the primary checkout mid-session — twice in one session, edits and test runs meant for the worktree landed on main instead. Operate in worktrees with explicit absolute paths throughout: `go -C <worktree-abs-path> test ...` for invocations, and absolute paths inside python heredocs/scripts. Confidence: 0.75
