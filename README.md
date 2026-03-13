# vnode

`vnode` is an open-source Kubernetes operator for building virtual node pools for virtual clusters.

It is designed for platform builders who want to offer isolated Kubernetes capacity to multiple tenants without provisioning a dedicated VM for every node in every cluster.

## What vnode is

- A CRD-driven operator that manages virtual node pools.
- A control plane for registering virtual nodes inside a target cluster.
- A translation layer that maps tenant-scheduled pods to workloads on a host cluster.
- A way to combine Kubernetes-native scheduling with stronger isolation backends such as Kata Containers.

## What vnode is not

- Not a custom container runtime.
- Not a patched `containerd` distribution.
- Not tied to a single cloud provider, datacenter, or network design.
- Not a hosted product.

## Problem

Virtual clusters are cheap to create, but node-based tenancy is still expensive if every tenant needs dedicated worker VMs.

`vnode` aims to close that gap by letting a platform operator expose virtual nodes inside a tenant cluster while deciding how workloads are placed and isolated on the underlying host cluster.

## Goals

- Per-tenant virtual node pools.
- Support for shared, dedicated, and hybrid pool policies.
- Strong workload isolation through pluggable runtimes.
- Clean lifecycle management with Kubernetes-native APIs.
- Generic primitives that can be mapped to many product or pricing models.

## Architecture

At a high level:

1. A platform operator creates a `VNodePool` custom resource.
2. `vnode` reconciles the desired virtual node count.
3. Each virtual node is registered into the target cluster.
4. Pods scheduled to those nodes are translated into host-cluster workloads.
5. Host pod status is synced back so the tenant cluster remains usable with normal Kubernetes tooling.

Core pieces:

- `VNodePool`: desired pool policy and capacity.
- `VNode`: one virtual node managed by the pool.
- Node registrar: connects to the target cluster and maintains virtual node presence.
- Pod translator: rewrites tenant pod specs for host execution.
- Status syncer: reflects host workload status back into the target cluster.

## API model

The current API is centered on two resources:

- `VNodePool`
  - target cluster reference
  - desired node count
  - per-node advertised CPU, memory, and pod capacity
  - pool mode such as `shared`, `dedicated`, or `burstable`
  - isolation backend and placement hints
- `VNode`
  - one virtual node owned by a pool
  - advertised capacity
  - lifecycle phase and conditions

The long-term direction is to keep the API generic and infrastructure-focused. Product packaging, billing plans, and provider branding should live outside this repository.

## Isolation model

`vnode` does not provide isolation by itself. It relies on isolation backends already installed in the host cluster.

Examples:

- Kata Containers
- gVisor
- other runtime-backed sandboxing strategies

The current default direction is to use Kata for stronger tenant boundaries when running on shared infrastructure.

## Project status

`vnode` is under active development.

What exists today:

- CRD types for `VNodePool` and `VNode`
- reconciliation flow for pool and node objects
- real target-cluster node registration from kubeconfig secrets
- target-cluster lease creation and cleanup
- translated host pod creation from tenant-scheduled pods
- host pod status sync back into tenant pods
- pool and pod execution conditions, events, and metrics
- domain model and service layer with unit and end-to-end tests
- runtime adapter abstraction

What is still being completed:

- host pod drift reconciliation is being tightened so source spec changes trigger safe host pod replacement
- stronger retry, conflict, and shutdown handling around status updates
- production-grade placement, cleanup, autoscaling, and installation packaging

## Design principles

- Kubernetes-native first
- no host mutation as a core requirement of the project itself
- separation between infrastructure primitives and product plans
- pluggable isolation and placement strategies
- honest status reporting over hidden magic

## Non-goals

- Building a proprietary all-in-one runtime stack
- Embedding billing or provider-specific commercial logic in the operator API
- Requiring a specific CNI, ingress, or datacenter topology

## Repository layout

```text
api/v1alpha1/              CRD type definitions
cmd/vnode/                 main entrypoint
internal/
  adapter/                 inbound and outbound adapters
  config/                  operator configuration
  domain/
    model/                 domain entities
    ports/                 interfaces
    service/               application/domain services
```

## Roadmap

See [ROADMAP.md](ROADMAP.md).

The short version:

1. Finish spec drift replacement behavior and shutdown-safe status reconciliation.
2. Add placement and lifecycle safety for shared and dedicated pools.
3. Add autoscaling and admission policy.
4. Harden installation, RBAC, and operational docs.

## Development

Build and test locally:

```bash
make test
make build
```

## License

Apache 2.0. See [LICENSE](LICENSE).
