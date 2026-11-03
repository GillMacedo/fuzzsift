.PHONY: test lint demo

RUN = go run ./plugin/scripts

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	cd examples/config && go vet ./...
	go test ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# size has failing entries on purpose, so it exits 1 with -fail.
demo:
	$(RUN) examples/config/size
	$(RUN) -fail examples/config/addr
	$(RUN) -fail examples/config/size || test $$? -eq 1
