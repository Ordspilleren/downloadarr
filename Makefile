# Build the Svelte app, then the Go binary with the assets embedded.
.PHONY: build web run tidy clean

build: web
	go build -o bin/downloadarr-lite ./cmd/downloadarr-lite

web:
	cd web && bun install && bun run build

run:
	go run ./cmd/downloadarr-lite

tidy:
	go mod tidy

clean:
	rm -rf bin internal/webui/dist
