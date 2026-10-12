# Lokal geliştirme portları. Backend'in kendi varsayılanı :8080'dir.
BACKEND_PORT ?= 8090
FRONTEND_PORT ?= 3000

.PHONY: backend frontend test lint build

backend:
	cd backend && UPTIME_ADDR=127.0.0.1:$(BACKEND_PORT) go run ./cmd/server

frontend:
	cd frontend && BACKEND_URL=http://127.0.0.1:$(BACKEND_PORT) npm run dev -- -p $(FRONTEND_PORT)

test:
	cd backend && go test ./...

lint:
	cd backend && go vet ./...
	cd frontend && npm run lint

build:
	cd backend && go build -o bin/server ./cmd/server
	cd frontend && npm run build
