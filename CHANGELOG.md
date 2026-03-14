# Changelog

## [1.4.3](https://github.com/KroderDev/vnode/compare/v1.4.2...v1.4.3) (2026-03-14)


### Bug Fixes

* **reconcile:** add hostname label to vnodes and reduce reconcile loop noise ([eb0b9c1](https://github.com/KroderDev/vnode/commit/eb0b9c1b93afb0e75f67687d46cf3af508556292))

## [1.4.2](https://github.com/KroderDev/vnode/compare/v1.4.1...v1.4.2) (2026-03-14)


### Bug Fixes

* **deletion:** best-effort deregistration so VNodePool deletion is not blocked ([a3d8c89](https://github.com/KroderDev/vnode/commit/a3d8c8988d900310e73c7f6ccf2ce4452694ef61))

## [1.4.1](https://github.com/KroderDev/vnode/compare/v1.4.0...v1.4.1) (2026-03-14)


### Bug Fixes

* **reconcile:** exponential backoff & skip ready node reconcile loop ([76f5ae8](https://github.com/KroderDev/vnode/commit/76f5ae8b5f6ef0c4c44f593956cf4cd8599784da))
* **reconcile:** non commited file ([723d2d6](https://github.com/KroderDev/vnode/commit/723d2d6f02f2ae858340ea6aee9e177b8b2b1e1d))

## [1.4.0](https://github.com/KroderDev/vnode/compare/v1.3.0...v1.4.0) (2026-03-14)


### Features

* **naming:** pool & vnode display name format ([77192e3](https://github.com/KroderDev/vnode/commit/77192e3d18d0470d7f9da2275d74adc361ab79dc))


### Bug Fixes

* vnode role defaults to worker ([978bb08](https://github.com/KroderDev/vnode/commit/978bb08fa80c3ae840a0024ddff2ba5e674ed8ba))
* workflow doesn't passing the build version ([d8b701d](https://github.com/KroderDev/vnode/commit/d8b701d2941d63cc0f4ff8c3e467faf651f2d3bf))

## [1.3.0](https://github.com/KroderDev/vnode/compare/v1.2.7...v1.3.0) (2026-03-14)


### Features

* show version and role on tenant virtual nodes ([dea2e4d](https://github.com/KroderDev/vnode/commit/dea2e4d7eb28b7241df5530595915a232a6e93a2))


### Bug Fixes

* add standard kubernetes labels to virtual nodes ([a664711](https://github.com/KroderDev/vnode/commit/a66471163553960a522853043a03d3265c78a81f))


### Documentation

* update README.md ([07dff0d](https://github.com/KroderDev/vnode/commit/07dff0d6d256673bb691b652a52a579394011594))
* update ROADMAP.md ([d7739e9](https://github.com/KroderDev/vnode/commit/d7739e9ece23481c74b3e7ae9cbccf8dbdffba02))

## [1.2.7](https://github.com/KroderDev/vnode/compare/v1.2.6...v1.2.7) (2026-03-14)


### Bug Fixes

* watch VNode phase changes to update pool status ([83b074a](https://github.com/KroderDev/vnode/commit/83b074aedb79df0f8feec5f3f4285bc72dafcf9f))

## [1.2.6](https://github.com/KroderDev/vnode/compare/v1.2.5...v1.2.6) (2026-03-13)


### Bug Fixes

* filter vnodepool reconciliation to spec-only changes ([747c150](https://github.com/KroderDev/vnode/commit/747c150dd8f9abebc0bf658ca8cb21a0c185d618))

## [1.2.5](https://github.com/KroderDev/vnode/compare/v1.2.4...v1.2.5) (2026-03-13)


### Bug Fixes

* filter vnodepool reconciliation to spec-only changes ([41397f4](https://github.com/KroderDev/vnode/commit/41397f4daaaae2d004db1bdfbfc86ff124cae23e))

## [1.2.4](https://github.com/KroderDev/vnode/compare/v1.2.3...v1.2.4) (2026-03-13)


### Bug Fixes

* decouple vnodepool from vnode status churn ([b5fa342](https://github.com/KroderDev/vnode/commit/b5fa342c3ec63fc6fbfe2674e1a944e6beb2deff))

## [1.2.3](https://github.com/KroderDev/vnode/compare/v1.2.2...v1.2.3) (2026-03-13)


### Bug Fixes

* requeue failed vnodes for recovery ([5c6db64](https://github.com/KroderDev/vnode/commit/5c6db6469370de24a9b45fdfbf3ed9126184c419))

## [1.2.2](https://github.com/KroderDev/vnode/compare/v1.2.1...v1.2.2) (2026-03-13)


### Bug Fixes

* stop vnodepool status hot loop ([2f5fade](https://github.com/KroderDev/vnode/commit/2f5fade285ab2c3162c15291be336effe00c05e2))

## [1.2.1](https://github.com/KroderDev/vnode/compare/v1.2.0...v1.2.1) (2026-03-13)


### Bug Fixes

* retry vnode registration after transient failures ([6da244f](https://github.com/KroderDev/vnode/commit/6da244fa27f1123f5cfca6ed18bd2c89551ebf16))

## [1.2.0](https://github.com/KroderDev/vnode/compare/v1.1.0...v1.2.0) (2026-03-13)


### Features

* add pod execution observability ([0f8e9df](https://github.com/KroderDev/vnode/commit/0f8e9dff7f8a4bf8d0ecabae1665e730b13e0cce))
* add vnode e2e coverage and harden reconciliation ([5ac85fc](https://github.com/KroderDev/vnode/commit/5ac85fc9e1f6975e92251d29f35c102fcf33a4b9))
* harden vnode execution reconciliation ([14b5044](https://github.com/KroderDev/vnode/commit/14b5044c31f275780a2ad0e072718f81e9ba58fb))
* **main:** support RuntimeClass, taints and tolerations in pools ([358a02d](https://github.com/KroderDev/vnode/commit/358a02d0c82e5ab7b335f63cb14974e42c41f60d))
* register tenant nodes and leases ([00a3fad](https://github.com/KroderDev/vnode/commit/00a3faddc25db01052a80fe46650043e8b422175))
* sync tenant pods to host workloads ([8e3a5a0](https://github.com/KroderDev/vnode/commit/8e3a5a093e4e6ec9ed62691c01f830148fee2d37))


### Bug Fixes

* stabilize envtest pod execution flow ([41c28b4](https://github.com/KroderDev/vnode/commit/41c28b4a84b65192998c944fe1f4e4a5509f4779))


### Documentation

* add README.md & ROADMAP.md ([bfca3f8](https://github.com/KroderDev/vnode/commit/bfca3f8cd77735ee667d5cd26f07295748110c0d))

## [1.1.0](https://github.com/KroderDev/vnode/compare/v1.0.0...v1.1.0) (2026-03-12)


### Features

* **main:** add vnodepool validation, kubeconfig resolver, deletion handling ([9ba2786](https://github.com/KroderDev/vnode/commit/9ba2786977450367621154ae40d09877873dd731))
* Phase 1 MVP - hexagonal architecture operator with 74 unit tests ([4be0b68](https://github.com/KroderDev/vnode/commit/4be0b6835972934df8ca7d1cd0f9abb8c3a33e8e))


### CI/CD

* add .release-please-manifest.json ([66e53a5](https://github.com/KroderDev/vnode/commit/66e53a530e9834270449b973fe86eef0e2c59b52))
