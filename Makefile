.PHONY: test test-race test-scenarios run example

# Unit + integration tests (no live Redis required for most packages)
test:
	go test ./...

test-race:
	go test -race ./...

# Live server scenarios — requires Redis + example server on :8080
test-scenarios:
	./scripts/run-scenarios.sh

# Start the example server (requires Redis)
run example:
	go run ./example

# Full local verify: unit tests then live scenarios
verify: test
	@echo "==> Checking Redis..."
	@redis-cli ping >/dev/null || (echo "Start Redis first: redis-server" && exit 1)
	@echo "==> Run './scripts/run-scenarios.sh' in another terminal while 'make run' is active"
