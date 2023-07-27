.PHONY: build
build: vet
	go build -o bin/imapstats ./...

.PHONY: install
install: test
	go install ./...

.PHONY: lint
lint:
	@golint ./...

.PHONY: vet
vet: lint
	@go vet ./...

.PHONY: test
test: build
	go test ./...
