# multitenancy
Multitenancy controller built using krtlite -- basically a clone of a portion of features from Capsule.

## Overview

This controller introduces two custom resources, `Tenant` and `TenantResource`. A `Tenant` is a set of namespaces which
are all subject to the same policies. Policies are enforced by ensuring each `TenantResource` is cloned into each 
namespace, where it is not subject to change. 

An example is below. 

```yaml
apiVersion: specs.kalexmills.com/v1alpha1
kind: Tenant
metadata:
  name: sample-tenant
spec:
  labels:
    example.org/tenant-class: dev
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: v1.33
  namespaces:
    - dev-tenant-1
    - dev-tenant-2
    - dev-tenant-3
  resources:
    - vault-secrets
    - dev-resource-quota
---
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