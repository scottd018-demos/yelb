VERSION ?= v0.1.0
push-app-image:
	docker build yelb-appserver --platform linux/amd64 -t quay.io/dscott0/yelb:$(VERSION)
	docker push quay.io/dscott0/yelb:$(VERSION)

push-ui-image:
	docker build yelb-ui --platform linux/amd64 -t quay.io/dscott0/yelb-ui:$(VERSION)
	docker push quay.io/dscott0/yelb-ui:$(VERSION)

YELB_DB_SERVER_ENDPOINT ?= proxy-free-tier-5xj.gcp-us-central1.cockroachlabs.cloud
YELB_DB_CLUSTER ?= demo-app-12182
YELB_DB_SERVER_PORT ?= 26257
YELB_DB_NAME ?= yelb
YELB_DB_USERNAME ?= yelb
YELB_DB_PASSWORD ?= 
REDIS_SERVER_ENDPOINT ?= redis-server
REDIS_PASSWORD ?=
REDIS_TLS ?= true
secret:
	kubectl create secret generic yelb \
		--from-literal=RACK_ENV=custom \
		--from-literal=YELB_DB_SERVER_ENDPOINT=$(YELB_DB_SERVER_ENDPOINT) \
		--from-literal=YELB_DB_SERVER_PORT=$(YELB_DB_SERVER_PORT) \
		--from-literal=YELB_DB_NAME=$(YELB_DB_NAME) \
		--from-literal=YELB_DB_USERNAME=$(YELB_DB_USERNAME) \
		--from-literal=YELB_DB_PASSWORD='$(YELB_DB_PASSWORD)' \
		--from-literal=REDIS_SERVER_ENDPOINT=$(REDIS_SERVER_ENDPOINT) \
		--from-literal=REDIS_PASSWORD='$(REDIS_PASSWORD)' \
		--from-literal=REDIS_TLS=$(REDIS_TLS) \

COCKROACH_CERT_FILE ?= root.crt
secret-cert:
	kubectl create secret generic yelb-cert --from-file=$(COCKROACH_CERT_FILE)

ingress:
	helm upgrade --install ingress-nginx ingress-nginx \
		--repo https://kubernetes.github.io/ingress-nginx \
		--namespace ingress-nginx \
		--create-namespace

kubernetes:
	kubectl apply -f deployments/platformdeployment/Kubernetes/yaml/yelb-k8s-custom.yaml

kubernetes-forward-service:
	kubectl apply -f deployments/platformdeployment/Kubernetes/yaml/yelb-port-forward.yaml

port-forward:
	kubectl -n ingress-nginx port-forward svc/ingress-nginx-controller-forward 8080:80