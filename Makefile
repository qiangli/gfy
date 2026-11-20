###########################################################################
##  GENERATED FILE — DO NOT EDIT.                                        ##
##  Produced from dag.md by `bashy dag --makefile`. Edit dag.md instead, ##
##  then regenerate; manual changes are overwritten and fail --check.    ##
###########################################################################
# Tasks live in dag.md (a bashy dag file); this Makefile only forwards.
#   bashy dag <target>           run a target (its dependencies first)
#   bashy dag --list             list targets
#   bashy dag --dry-run <target> print the plan, run nothing
#   bashy dag --skill DIR        export these tasks as an Agent Skill
# Regenerate with: bashy dag --makefile  (verify: bashy dag --makefile --check)

# List the dag targets, then how to run them natively.
help: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag --list; echo "Run: bashy dag <target> | skill: bashy dag --skill DIR | more: bashy help"

# Build the gfy binary into ./gfy, injecting the version via ldflags.
build: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag build

# Run go mod tidy, go fmt, and go vet over cmd, pkg and internal.
tidy: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag tidy

# Run all tests.
test: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag test

# Run golangci-lint.
lint: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag lint

# Install gfy to $GOPATH/bin with the version ldflags.
install: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag install

# Compare the local working tree against the remote tracking branch.
diff: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag diff

# Remove build artifacts (the gfy binary and .gfy-out).
clean: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag clean

# The gofmt gate (reports unformatted files, never rewrites them), plus a check that the generated Makefile still matches this file — regenerate with `bashy dag…
fmtcheck: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag fmtcheck

# Install the pre-push formatting gate.
hooks: FORCE
	@command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag hooks

.DEFAULT: ; @command -v bashy >/dev/null || { echo "requires bashy: https://github.com/qiangli/bashy"; exit 127; }; bashy dag $@

# FORCE names no file and has no recipe: as the one prerequisite it
# keeps every forward running even when a file or directory on disk
# shares a target's name. Dependency order stays in dag, not here.
FORCE:
