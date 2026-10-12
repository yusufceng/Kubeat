# Lokal geliştirme portları. Backend'in kendi varsayılanı :8080'dir.
BACKEND_PORT ?= 8090
FRONTEND_PORT ?= 3000
# Lokal geliştirmede backend ile frontend arasındaki ortak anahtar. Gerçek
# kurulumda chart rastgele bir anahtar üretir.
API_TOKEN ?= local-dev-token
# Lokal geliştirmede arayüzün yönetici parolası. Boş bırakılırsa giriş
# istenmez. Gerçek kurulumda chart rastgele bir parola üretir.
ADMIN_PASSWORD ?= admin

# Cluster hedefleri her zaman bu bağlamı kullanır; kubectl'in o anki bağlamını
# asla kullanmaz.
KUBE_CONTEXT ?= minikube
NAMESPACE ?= kubeat
VERSION ?= 0.12.0
CHART := deploy/helm/kubeat
# Ek helm ayarları, örn. HELM_ARGS="--set prometheus.url=http://..."
HELM_ARGS ?=

.PHONY: dev backend frontend test lint build images minikube-deploy minikube-open minikube-demo minikube-password

# Backend ve frontend'i birlikte çalıştırır; Ctrl-C ikisini de durdurur.
dev:
	@trap 'kill 0' INT TERM EXIT; $(MAKE) backend & $(MAKE) frontend & wait

backend:
	cd backend && UPTIME_ADDR=127.0.0.1:$(BACKEND_PORT) UPTIME_API_TOKEN=$(API_TOKEN) go run ./cmd/server

frontend:
	cd frontend && BACKEND_URL=http://127.0.0.1:$(BACKEND_PORT) BACKEND_TOKEN=$(API_TOKEN) ADMIN_PASSWORD=$(ADMIN_PASSWORD) npm run dev -- -p $(FRONTEND_PORT)

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
		--set podSecurityContext.fsGroup=1001 $(HELM_ARGS) --wait

# Arayüzü http://localhost:$(FRONTEND_PORT) adresine açar.
minikube-open:
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) port-forward svc/kubeat-frontend $(FRONTEND_PORT):3000

# Denemek için örnek bir namespace (shop-demo) ve pod metrikleri için küçük
# bir Prometheus (monitoring-demo) kurar.
minikube-demo:
	kubectl --context $(KUBE_CONTEXT) apply -f deploy/demo/shop.yaml -f deploy/demo/prometheus.yaml

# Minikube kurulumunun yönetici parolasını yazdırır.
minikube-password:
	@kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) get secret kubeat-admin -o jsonpath='{.data.password}' | base64 -d; echo
