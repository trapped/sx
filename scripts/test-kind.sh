#!/usr/bin/env bash

# This script is used for testing configuration autoreload with Kubernetes
# ConfigMaps:
#
# 1. spins up a kind (Kubernetes in Docker) cluster
# 2. deploys SX using kubectl and the default manifests in kubernetes/
# 3. checks SX logs for the word "listen"
# 4. applies a new ConfigMap
# 5. waits a few seconds, then checks SX logs searching for the word "reload"

set -euxo pipefail

# clean workspace just in case
kind delete cluster

# create Kubernetes cluster; automatically configures kubectl
kind create cluster --config kind-config.yml

# load latest image into kind
kind load docker-image ghcr.io/trapped/sx:latest

# apply manifests
kubectl apply -f kubernetes/namespace.yml
kubectl apply -f kubernetes/configmap-facts.yml
kubectl apply -f kubernetes/service.yml
kubectl apply -f kubernetes/deployment-neverpull.yml

# wait for SX to start
kubectl rollout status --namespace=sx deploy/sx --timeout=300s

# check SX is up
if ! kubectl logs --namespace=sx deploy/sx | grep "listen"; then
    echo "ERROR: SX does not appear up yet"
    exit 1
fi

# apply a new config
kubectl apply -f kubernetes/configmap-catpics.yml

# check SX reloaded its config
if ! timeout 300s grep -m1 reload <(kubectl logs -f --namespace=sx deploy/sx | tee /dev/stderr); then
    echo "ERROR: SX has not reloaded its config yet"
    exit 1
fi
