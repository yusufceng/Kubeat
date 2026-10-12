# Lokal geliştirme portları. Backend'in kendi varsayılanı :8080'dir.
BACKEND_PORT ?= 8090
FRONTEND_PORT ?= 3000

# Cluster hedefleri her zaman bu bağlamı kullanır; kubectl'in o anki bağlamını
# asla kullanmaz.
KUBE_CONTEXT ?= minikube
NAMESPACE ?= kubeat
VERSION ?= 0.6.0
CHART := deploy/helm/kubeat

.PHONY: dev backend frontend test lint build images minikube-deploy minikube-open minikube-demo

# Backend ve frontend'i birlikte çalıştırır; Ctrl-C ikisini de durdurur.
dev:
	@trap 'kill 0' INT TERM EXIT; $(MAKE) backend & $(MAKE) frontend & wait

backend:
	cd backend && UPTIME_ADDR=127.0.0.1:$(BACKEND_PORT) go run ./cmd/server

frontend:
	cd frontend && BACKEND_URL=http://127.0.0.1:$(BACKEND_PORT) npm run dev -- -p $(FRONTEND_PORT)

test:
	cd backend && go test ./...

lint:
	cd backend && go vet ./...
	cd frontend && npm run lint
	helm lint $(CHART)

build:
	cd backend && go build -o bin/server ./cmd/server
	cd frontend && npm run build

images:
	docker build -t kubeat-backend:$(VERSION) --build-arg VERSION=$(VERSION) backend
	docker build -t kubeat-frontend:$(VERSION) frontend

# İmajları minikube'a yükler ve chart'ı kurar ya da günceller.
minikube-deploy: images
	minikube image load kubeat-backend:$(VERSION) kubeat-frontend:$(VERSION)
	helm --kube-context $(KUBE_CONTEXT) upgrade --install kubeat $(CHART) \
		--namespace $(NAMESPACE) --create-namespace \
		--set backend.image.tag=$(VERSION) --set frontend.image.tag=$(VERSION) \
		--set podSecurityContext.fsGroup=1001 --wait

# Arayüzü http://localhost:$(FRONTEND_PORT) adresine açar.
minikube-open:
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) port-forward svc/kubeat-frontend $(FRONTEND_PORT):3000

# Keşfi denemek için örnek bir namespace (shop-demo) kurar.
minikube-demo:
	kubectl --context $(KUBE_CONTEXT) apply -f deploy/demo/shop.yaml
