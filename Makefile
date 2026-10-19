# Build the Svelte app, then the Go binary with the assets embedded.
.PHONY: build web check run tidy clean

build: web
	go build -o bin/downloadarr ./cmd/downloadarr

web:
	cd web && bun install && bun run check && bun run build

check:
	cd web && bun run check

run:
	go run ./cmd/downloadarr

tidy:
	go mod tidy

clean:
	rm -rf bin internal/webui/dist
