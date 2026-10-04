.PHONY: test vet integration check

MODULES := logger feishu config httpclient retry lifecycle llm apperror storage download

test:
	@set -e; for module in $(MODULES); do (cd "$$module" && GOWORK=off go test -race -cover ./...); done

vet:
	@set -e; for module in $(MODULES); do (cd "$$module" && GOWORK=off go vet ./...); done

integration:
	go test -race ./tests/*.go

check: test vet integration
