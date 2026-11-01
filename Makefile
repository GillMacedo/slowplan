.PHONY: test lint demo

INSPECTOR = npx -y @modelcontextprotocol/inspector@2.13.0 --cli /tmp/slowplan serve

test:
	test -z "$$(gofmt -l main.go main_test.go internal)"
	go vet ./...
	go test -race ./...

lint:
	go build -o /tmp/slowplan .
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict
	$(INSPECTOR) --protocol-era modern --method tools/list --strict
	$(INSPECTOR) --protocol-era legacy --method tools/list --strict

demo:
	go run . review examples/index-then-filter.txt
