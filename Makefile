.PHONY: test vet integration check vuln stress fuzz

STRESS_COUNT ?= 20
FUZZTIME ?= 10s

MODULES := ssh version artifact cache/redis cache persistcache safemap filetree utils metrics middleware ratelimit workerpool logger feishu config httpclient retry lifecycle llm apperror storage download

test:
	@set -e; for module in $(MODULES); do (cd "$$module" && GOWORK=off go test -race -cover ./...); done

vet:
	@set -e; for module in $(MODULES); do (cd "$$module" && GOWORK=off go vet ./...); done

integration:
	go test -race ./tests/*.go

check: test vet integration

# Repeat concurrent state-machine and shutdown tests under the race detector.
stress:
	go test -race -count=$(STRESS_COUNT) -timeout=2m ./workerpool/... ./cache ./ratelimit/... ./persistcache/... ./safemap/... ./feishu/dedup ./llm/chat ./logger/...

# Limit worker count so these targets also fit small CI runners.
fuzz:
	cd download && GOWORK=off go test -run='^$$' -fuzz=FuzzParseContentRange -fuzztime=$(FUZZTIME) -parallel=2 .
	cd llm && GOWORK=off go test -run='^$$' -fuzz=FuzzToolArguments -fuzztime=$(FUZZTIME) -parallel=2 ./chat

vuln:
	@set -e; for module in $(MODULES); do (cd "$$module" && GOWORK=off govulncheck ./...); done
