// Package provider implements the Virtual Kubelet PodLifecycleHandler and NodeProvider
// interfaces for the vnode project. It translates vcluster pod operations into
// real pods on a host cluster with Kata Containers RuntimeClass.
package provider
