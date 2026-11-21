# payload

`desktop/build/package.sh` writes `app.tar.gz` here — the built ASG desktop app, laid
out as it should appear in the install folder — and then builds the setup wizard,
which embeds it. The archive is git-ignored.

Without it, `go build` still works: the wizard runs as a preview that walks through
every screen but installs nothing.
