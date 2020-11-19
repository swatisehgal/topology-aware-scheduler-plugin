COMMONENVVAR=GOOS=linux GOARCH=amd64
BUILDENVVAR=CGO_ENABLED=0

.PHONY: all
all: build

.PHONY: build
build: gofmt
	$(COMMONENVVAR) $(BUILDENVVAR) go build -ldflags '-w' -o bin/kube-scheduler main.go

.PHONY: gofmt
gofmt:
	@echo "Running gofmt"
	gofmt -s -w `find . -path ./vendor -prune -o -type f -name '*.go' -print`

.PHONY: image
image: build
	@echo "building image"
	docker build -f images/Dockerfile -t quay.io/swsehgal/ta-scheduler:latest .

.PHONY: push
push: image
	@echo "pushing image"
	docker push quay.io/swsehgal/ta-scheduler:latest

.PHONY: crd
crd:
		@echo "deploying crd"
		kubectl create -f manifests/crd-v1alpha1.yaml
		kubectl create -f manifests/node-crd.yaml
		kubectl create -f manifests/master-crd.yaml

.PHONY: deploy
deploy: push
		@echo "Deploying scheduler"
		kubectl create -f manifests/my-scheduler-configmap.yaml
		kubectl create -f manifests/deploy.yaml

.PHONY: deploy-pod
deploy-pod:
		kubectl create -f manifests/test-deployment.yaml

clean-binaries:
	rm -f bin/kube-scheduler

clean: clean-binaries
	kubectl delete -f manifests/deploy.yaml
	kubectl delete -f manifests/my-scheduler-configmap.yaml
