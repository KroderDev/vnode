# Roadmap

This roadmap reflects the intended direction of `vnode` as an open-source infrastructure primitive.

It avoids product-specific packaging and focuses on what the operator must do well to be useful and safe.

## Mission

`vnode` should let a platform builder expose virtual nodes inside tenant clusters while keeping control of isolation, placement, and capacity on a shared host cluster.

## Scope boundary

`vnode` should own:

- virtual node lifecycle
- target-cluster registration
- pod translation and host execution
- placement policy hooks
- status synchronization
- pool safety, cleanup, and observability

`vnode` should not own:

- billing logic
- provider branding such as AWS, GCP, or DigitalOcean plan names
- customer-facing product catalog semantics

Those belong in higher-level platform services that translate commercial plans into `VNodePool` policy.

## Revised phases

### Phase 1: Operator foundation

Status: mostly complete

Objectives:

- define `VNodePool` and `VNode` CRDs
- implement reconcile loops
- establish domain model and service boundaries
- implement basic validation and unit tests

Exit criteria:

- CRDs generated and installable
- operator starts and reconciles objects
- unit tests cover domain logic

### Phase 2: Real execution path

Status: mostly complete

Objectives:

- replace stub registration with real target-cluster node registration
- create a real per-tenant connection path from kubeconfig secrets
- implement pod lifecycle handling for translated workloads on the host cluster
- sync host pod status back to the target cluster
- ensure teardown removes nodes, leases, and translated workloads cleanly

Exit criteria:

- a `VNodePool` creates Ready nodes in a target cluster
- scheduling a pod to a virtual node results in host execution
- tenant-side pod status reflects host-side execution
- deleting a pool leaves no orphaned node or workload artifacts

Current state:

- tenant kubeconfig resolution is wired into a cached tenant client manager
- `VNode` reconciliation registers real target-cluster Nodes and Leases
- translated host pods are created and cleaned up
- host pod status is mirrored back into tenant pods
- pod execution conditions, events, and metrics exist

Remaining work in this phase:

- finish host pod drift replacement behavior for tenant pod spec changes
- reduce conflict and shutdown noise in status and cleanup paths
- tighten cleanup behavior for reschedules and partial failures

### Phase 3: Placement and tenancy policy

Objectives:

- make shared, dedicated, and burstable pool behavior real instead of descriptive
- introduce explicit host placement controls
- support taints, tolerations, selectors, and optional policy references
- add safe scale-down behavior that avoids deleting active capacity blindly
- define capacity and oversubscription rules intentionally

Exit criteria:

- dedicated pools can be pinned to reserved capacity
- shared pools can be scheduled with clear placement and isolation rules
- scale-down honors safety constraints
- status surfaces usable placement and capacity information

### Phase 4: Production hardening

Objectives:

- admission validation and defaulting
- structured conditions and event reporting
- Prometheus metrics
- Helm chart and installation docs
- end-to-end tests with virtual clusters
- RBAC tightening and security review
- shutdown-tolerant reconcile behavior and conflict-safe status writes

Exit criteria:

- repeatable install path
- observable reconciliation and runtime behavior
- validated failure and cleanup flows

### Phase 5: Autoscaling

Objectives:

- min/max node counts
- pending-pod driven scale-up
- underutilization-aware scale-down
- cooldowns and policy guardrails

Exit criteria:

- pools scale predictably under load
- autoscaling does not break isolation or cleanup guarantees

## Recommended API direction

Keep the public API generic.

Recommended focus areas for future fields:

- placement policy
- runtime class or isolation policy reference
- taints and tolerations
- scale-down policy
- readiness and cleanup conditions
- autoscaling bounds

Avoid baking customer-facing plan names or cloud-provider labels into the operator API.

## Migration guidance

If another system currently deploys per-tenant vnode workloads directly, the end-state should be:

1. create the target virtual cluster
2. publish its kubeconfig secret
3. create a `VNodePool`
4. watch `VNodePool.status`

That keeps vnode lifecycle in one place and avoids duplicate control logic.
