# The scanner's own checks. The smoke test runs bin/smells with an empty cache on a copy
# of the fixture project, which proves the launcher builds the scanner and runs it.
export GOFLAGS := -mod=readonly

check: test smoke

test:
	test -z "$$(gofmt -l scanner/*.go)" || { gofmt -l scanner/*.go; exit 1; }
	cd scanner/typescript && bun install --frozen-lockfile --silent
	cd scanner && go vet ./...
	@log=$$(mktemp) && trap 'rm -f "$$log"' EXIT; \
	status=0; (cd scanner && go test -v -count=1 -timeout 2m ./...) >"$$log" 2>&1 || status=$$?; \
	cat "$$log"; \
	awk '/^func Test/ { name=$$2; sub(/\(.*/, "", name); file[name]=FILENAME } \
		$$1 == "---" && $$2 ~ /^(PASS|FAIL|SKIP):$$/ { \
			seconds=$$4; gsub(/[()s]/, "", seconds); \
			print "Tests", seconds, $$3; \
			if ($$3 !~ /\// && $$3 in file) total[file[$$3]]+=seconds; \
		} \
		END { for (path in total) print "Files", total[path], path }' scanner/*_test.go "$$log" | \
	LC_ALL=C sort -k1,1 -k2,2nr -k3,3 | \
	awk '$$1 != section { section=$$1; print "Slowest 10 " tolower(section) " (seconds):" } \
		++count[$$1] <= 10 { printf "  %.2f %s\n", $$2, $$3 }' || echo 'Timing report unavailable'; \
	exit $$status

smoke:
	@dir=$$(mktemp -d) && trap 'rm -rf "$$dir"' EXIT && cp -R scanner/testdata/mixed/. "$$dir/project" && \
	export XDG_CACHE_HOME="$$dir/cache" && cd "$$dir/project" && git init -q && "$(CURDIR)/bin/smells" baseline && mv .smells.candidate.json .smells.json && git add -A && \
	git -c user.name=smoke -c user.email=smoke@example.com commit -qm fixture && \
	git update-ref refs/remotes/origin/main HEAD && git symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/main && \
	"$(CURDIR)/bin/smells" check

.PHONY: check test smoke
