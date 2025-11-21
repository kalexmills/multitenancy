# multitenancy
Sample controller built using [krt-lite](https://github.com/kalexmills/krt-lite).

## Overview

This project is presented as a sample controller built using krt-lite -- it is not intended for production use.

A Kubernetes controller implementing a few minimal features for soft multitenancy. Allows users to keep resources for
multitenancy in-sync across a tenant's namespaces.

## Getting Started

We'll start by setting up an environment and demonstrating what the controller does.

Checkout the repository and ensure [go](https://go.dev/doc/install), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/),
[make](https://www.gnu.org/software/make/), [kubectl](https://kubernetes.io/docs/tasks/tools/), and [helm](https://helm.sh/docs/intro/install/) 
are all installed. This project uses [docker](https://docs.docker.com/get-started/get-docker/) to build container 
images.

Create the kind cluster using `make kind-create`. Once a cluster is started, you can use `make kind-refresh` to
build and install the controller to the cluster.

### Tenants

The multitenancy controller introduces two custom resources, `Tenant` and `TenantResource`. A `Tenant` is a set of
namespaces which are all subject to the same policies. Policies are enforced by ensuring a copy of each `TenantResource`
is placed into each namespace, where it is not subject to change.

A `Tenant` describes a list of namespaces, along with a set of resources and labels which each namespace must have.
`spec.labels` contains a list of labels which are added to the namespace. `spec.resources` lists `TenantResources` which
should be created in each namespace by name.

```yaml
apiVersion: specs.kalexmills.com/v1alpha1
kind: Tenant
metadata:
  name: sample-tenant
spec:
  namespaces:
    - dev-tenant-1
    - dev-tenant-2
    - dev-tenant-3
  labels:
    example.org/tenant-class: dev
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: v1.33
  resources:
    - vault-secrets
    - dev-resource-quota
```

Copy the contents above into a file named `sample-tenant.yaml` and apply it to the cluster using `kubectl apply -f sample-tenant.yaml`.
You should see three new namespaces created, each one should have the labels that we specified.

```
$ kubectl get namespaces -l example.org/tenant-class
NAME                 STATUS   AGE
dev-tenant-1         Active   38s
dev-tenant-2         Active   38s
dev-tenant-3         Active   38s
```

The `Tenant` we created above names two `TenantResources`, but neither has been created yet. We'll do so in the next
section.

### TenantResources

A `TenantResource` describes a Kubernetes resource which is automatically copied into tenant namespaces. Changes to
TenantResource definitions are automatically applied to all copies. Adding or removing a TenantResource to/from a 
Tenant results in the corresponding object being created or removed in the namespace.

Examples can be found below. `dev-resource-quota` describes a ResourceQuota, while `vault-secrets` describes a secret. 

```yaml
apiVersion: specs.kalexmills.com/v1alpha1
kind: TenantResource
metadata:
  name: dev-resource-quota
spec:
  resource:
    group: ""
    version: v1
    resource: resourcequotas
  manifest:
    apiVersion: v1
    kind: ResourceQuota
    metadata:
      name: dev-resource-quota
    spec:
      hard:
        cpu: "5"
        memory: "10Gi"
        pods: "10"
---
apiVersion: specs.kalexmills.com/v1alpha1
kind: TenantResource
metadata:
  name: vault-secrets
spec:
  resource:
    group: ""
    version: v1
    resource: secrets
  manifest:
    apiVersion: v1
    kind: Secret
    metadata:
      name: vault-access-key
    data:
      key: "c3VwZXItc2VjcmV0LXZhbHVlCg=="
```

Copy the above into a file named `sample-resources.yaml` and apply it using `kubectl apply -f sample-resources.yaml`.
You should see ResourceQuotas and Secrets created in each namespace.

```
$ kubectl get resourcequota -A -l multitenancy/tenant
NAMESPACE      NAME                 AGE  REQUEST                                LIMIT
dev-tenant-1   dev-resource-quota   1m   cpu: 0/5, memory: 0/10Gi, pods: 0/10   
dev-tenant-2   dev-resource-quota   1m   cpu: 0/5, memory: 0/10Gi, pods: 0/10   
dev-tenant-3   dev-resource-quota   1m   cpu: 0/5, memory: 0/10Gi, pods: 0/10   

$ kubectl get secrets -A -l multitenancy/tenant
NAMESPACE      NAME               TYPE     DATA   AGE
dev-tenant-1   vault-access-key   Opaque   1      19h
dev-tenant-2   vault-access-key   Opaque   1      50m
dev-tenant-3   vault-access-key   Opaque   1      19h
```

`TenantResources` are persistent. Attempts to update or delete them result in the resources being recreated or
reverted back to their desired state. To demonstrate this, run the following command to watch resource quotas.

```
$ kubectl get resourcequotas -A -l multitenancy/tenant --watch
NAMESPACE      NAME                 AGE     REQUEST                                LIMIT
dev-tenant-1   dev-resource-quota   101s    cpu: 0/5, memory: 0/10Gi, pods: 0/10   
dev-tenant-2   dev-resource-quota   3m22s   cpu: 0/5, memory: 0/10Gi, pods: 0/10   
dev-tenant-3   dev-resource-quota   19h     cpu: 0/5, memory: 0/10Gi, pods: 0/10   
dev-tenant-1   dev-resource-quota   2m35s   cpu: 0/5, memory: 0/10Gi, pods: 0/10   
 
```

From a separate terminal, delete one of the ResourceQuotas.

```
$ kubectl delete resourcequota dev-resource-quota -n dev-tenant-1
```

You should see two events in the terminal running the watch. The first is from deleting the resource, the second is from
the multitenancy controller recreating the resource.

```
dev-tenant-1   dev-resource-quota   0s                                             
dev-tenant-1   dev-resource-quota   0s      cpu: 0/5, memory: 0/10Gi, pods: 0/10   
```

## Where are the tests?

This entire repository is an experiment to test the API of [krt-lite](https://github.com/kalexmills/krt-lite). In a way,
the whole repo is intended as test code. Should we test the test? Maybe, but I don't wash my soap.

THat said, once things are more mature this repo should definitely demonstrate some patterns for effective testing.
