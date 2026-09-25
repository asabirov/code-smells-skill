# The scanner's own checks. The smoke test runs bin/smells with an empty cache on a copy
# of the fixture project, which proves the launcher builds the scanner and runs it.
export GOFLAGS := -mod=readonly

check: test smoke

test:
	test -z "$$(gofmt -l scanner/*.go)" || { gofmt -l scanner/*.go; exit 1; }
	cd scanner/typescript && bun install --frozen-lockfile --silent
	cd scanner && go vet ./... && go test -count=1 -timeout 2m ./...

smoke:
	@dir=$$(mktemp -d) && trap 'rm -rf "$$dir"' EXIT && cp -R scanner/testdata/mixed/. "$$dir/project" && \
	export XDG_CACHE_HOME="$$dir/cache" && cd "$$dir/project" && git init -q && "$(CURDIR)/bin/smells" baseline && mv .smells.candidate.json .smells.json && git add -A && \
	git -c user.name=smoke -c user.email=smoke@example.com commit -qm fixture && \
	git update-ref refs/remotes/origin/main HEAD && "$(CURDIR)/bin/smells" check

.PHONY: check test smoke
