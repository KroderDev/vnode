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

## Phases

### Phase 1: Operator foundation

Status: **complete**

- `VNodePool` and `VNode` CRDs defined and installable
- Reconcile loops for pool, node, and pod sync
- Hexagonal domain model with ports and adapters
- Unit tests covering domain logic

### Phase 2: Real execution path

Status: **complete**

- Tenant kubeconfig resolution from Kubernetes Secrets via cached client manager
- Real target-cluster Node and Lease registration
- Virtual nodes appear with correct capacity, labels (`kubernetes.io/os`, `kubernetes.io/arch`), role (`node-role.kubernetes.io/vnode`), and version info (`vnode/<release>`)
- Translated host pods created with configurable RuntimeClass (default: Kata)
- Host pod status mirrored back into tenant pods
- Pod execution conditions, events, and Prometheus metrics
- Pool deletion with finalizer-based cleanup (nodes, leases, host pods)
- VNode self-healing with 2s requeue for failed registrations
- VNodePool reconcile decoupled from status churn (GenerationChangedPredicate + phase-only VNode watch)
- Build-time version injection via ldflags

### Phase 3: Placement and tenancy policy

Status: **in progress**

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

- admission validation and defaulting webhooks
- Helm chart and installation docs
- end-to-end tests with real virtual clusters
- RBAC tightening and security review
- shutdown-tolerant reconcile behavior and conflict-safe status writes
- host pod drift reconciliation (spec changes trigger safe replacement)
- structured error handling for transient failures

Exit criteria:

- repeatable install path via Helm
- observable reconciliation and runtime behavior
- validated failure and cleanup flows
- comprehensive RBAC with least-privilege

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
