GO ?= go
TINYGO ?= tinygo
TARGET = ./targets/esp32c6-radio.json

.PHONY: check lint build test firmware thread-link consumer probes

check:
	$(MAKE) lint
	$(MAKE) build
	$(MAKE) test
	$(MAKE) firmware
	$(MAKE) thread-link
	$(MAKE) probes

lint:
	@files="$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go'))"; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi
	$(GO) vet ./...
	$(GO) run ./tools/import-native

build:
	$(GO) build ./...

test:
	$(GO) test -race -count=1 ./...

firmware:
	@mkdir -p build
	@set -eu; for example in bringup raw-radio; do \
		$(TINYGO) build -target $(TARGET) -size short -o build/$$example.elf ./examples/$$example; \
		$(TINYGO) build -target $(TARGET) -o build/$$example.bin ./examples/$$example; \
		$(GO) run ./tools/check-image build/$$example.elf build/$$example.bin; \
	done
	$(TINYGO) build -target $(TARGET) -ldflags="-X main.localAddress=0x5678 -X main.peerAddress=0x1234" -o build/raw-b.elf ./examples/raw-radio
	$(TINYGO) build -target $(TARGET) -ldflags="-X main.localAddress=0x5678 -X main.peerAddress=0x1234" -o build/raw-b.bin ./examples/raw-radio
	$(GO) run ./tools/check-image build/raw-b.elf build/raw-b.bin

thread-link:
	@mkdir -p build
	$(TINYGO) build -target $(TARGET) -size short -o build/thread-link.elf ./tools/thread-link
	$(TINYGO) build -target $(TARGET) -o build/thread-link.bin ./tools/thread-link
	$(GO) run ./tools/check-image build/thread-link.elf build/thread-link.bin

consumer:
	$(GO) run ./tools/check-consumer
	$(GO) run ./tools/check-consumer -example=storage

probes:
	@set -eu; for probe in storage entropy rf-validation thread-validation; do \
		$(GO) run ./tools/lab build -probe $$probe -out build/lab/$$probe; \
	done
