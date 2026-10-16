# Lokal geliştirme portları. Backend'in kendi varsayılanı :8080'dir.
BACKEND_PORT ?= 8090
FRONTEND_PORT ?= 3000
# Lokal geliştirmede backend ile frontend arasındaki ortak anahtar. Gerçek
# kurulumda chart rastgele bir anahtar üretir.
API_TOKEN ?= local-dev-token
# Lokal geliştirmede ilk yönetici hesabı. Boşsa ilk açılışta arayüz /setup
# sayfasında ilk yöneticiyi oluşturtur. Verilirse backend hesap yokken "admin"
# hesabını bu parolayla açar; parola kurallara uymalıdır (en az 8 karakter,
# "admin" ya da "password" gibi ilk denenenlerden biri değil), yoksa backend
# başlamaz. Gerçek kurulumda chart rastgele bir parola üretir.
ADMIN_PASSWORD ?=
# Arayüzün dinlediği adres: yalnızca bu makineden erişilsin diye 127.0.0.1.
# Ağdaki başka bir cihazdan denemek için FRONTEND_HOST=0.0.0.0.
FRONTEND_HOST ?= 127.0.0.1
# Lokal veritabanındaki gizli değerleri şifreleyen anahtar; ilk çalıştırmada
# bir kez üretilir ve git'e girmez. Silinirse veritabanındaki şifreli değerler
# okunamaz ve backend başlamaz: o zaman backend/uptime.db de silinmelidir.
DEV_SECRET_KEY := .dev/secret.key
# Lokal geliştirmede bu makinedeki servisler de kontrol edilebilsin diye
# loopback hedef politikasından istisna tutulur. Gerçek kurulumda loopback
# yasaktır (README, "Hedef politikası").
TARGET_ALLOW_CIDRS ?= 127.0.0.0/8,::1/128

# Cluster hedefleri her zaman bu bağlamı kullanır; kubectl'in o anki bağlamını
# asla kullanmaz.
KUBE_CONTEXT ?= minikube
NAMESPACE ?= kubeat
VERSION ?= 0.37.0
CHART := deploy/helm/kubeat
# Ek helm ayarları, örn. HELM_ARGS="--set prometheus.url=http://..."
HELM_ARGS ?=
# Minikube'da backend'in volume'una yazabilmesi için gereken ayar. OpenShift
# benzetimi bunu kendi değerleriyle değiştirir.
MINIKUBE_SECURITY ?= --set podSecurityContext.fsGroup=1001

.PHONY: dev backend frontend test lint build images minikube-deploy minikube-open minikube-demo minikube-password minikube-openshift-sim

# Backend ve frontend'i birlikte çalıştırır; Ctrl-C ikisini de durdurur.
dev:
	@trap 'kill 0' INT TERM EXIT; $(MAKE) backend & $(MAKE) frontend & wait

backend: $(DEV_SECRET_KEY)
	cd backend && UPTIME_ADDR=127.0.0.1:$(BACKEND_PORT) UPTIME_API_TOKEN=$(API_TOKEN) UPTIME_ADMIN_PASSWORD=$(ADMIN_PASSWORD) \
		UPTIME_SECRET_KEY_FILE=$(abspath $(DEV_SECRET_KEY)) UPTIME_TARGET_ALLOW_CIDRS=$(TARGET_ALLOW_CIDRS) go run ./cmd/server

$(DEV_SECRET_KEY):
	@mkdir -p $(@D)
	@umask 077 && openssl rand -base64 36 > $@.partial && mv $@.partial $@
	@echo "$@ oluşturuldu: lokal veritabanının şifreleme anahtarı"

frontend:
	cd frontend && BACKEND_URL=http://127.0.0.1:$(BACKEND_PORT) npm run dev -- -p $(FRONTEND_PORT) -H $(FRONTEND_HOST)

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
		--set backend.image.repository=kubeat-backend --set frontend.image.repository=kubeat-frontend \
		--set backend.image.tag=$(VERSION) --set frontend.image.tag=$(VERSION) \
		$(MINIKUBE_SECURITY) $(HELM_ARGS) --wait

# Minikube'u Kubeat'in gözünde OpenShift'e benzetir (Route ve ServiceMonitor
# CRD'leri, token ve TLS isteyen bir thanos-querier, kısıtlı pod güvenliği) ve
# chart'ı OpenShift'teki ayarlarıyla ayrı bir namespace'e kurar. Önce
# minikube-demo gerekir. Kaldırmak için: helm -n kubeat-ocp uninstall kubeat
minikube-openshift-sim: images
	KUBE_CONTEXT=$(KUBE_CONTEXT) deploy/demo/openshift-sim/install.sh
	$(MAKE) minikube-deploy NAMESPACE=kubeat-ocp MINIKUBE_SECURITY= \
		HELM_ARGS="-f deploy/demo/openshift-sim/values.yaml $(HELM_ARGS)"

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

# Sürümü etiketler ve iter; GitHub Actions imajları ghcr.io'ya, chart'ı
# oci://ghcr.io/yusufceng/charts'a yayımlar (.github/workflows/release.yml).
release:
	@test "$$(git branch --show-current)" = main || (echo "release main'den yapılır"; exit 1)
	@test -z "$$(git status --porcelain)" || (echo "çalışma ağacı temiz değil"; exit 1)
	git tag -a v$(VERSION) -m "Kubeat $(VERSION)"
	git -c http.sslVerify=true push origin v$(VERSION)
