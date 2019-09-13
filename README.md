SELinux policy helper operator
==============================

This is a controller that listens to all pods in the system.

It will attempt to generate a policy for pods when the pod is annotated with
a specific tag "generate-selinux-policy" and the pod is in a running
state.

In order to generate the policy, it spawns a pod with the selinux-k8s
tool which uses udica to generate the policy. It will spit out a
configmap with the appropriate policy.

Instructions
------------
Build:

```
make
```

Create assets in cluster:

```
oc create -f ns.yaml
oc project selinux-policy-helper-operator
oc create -f sa.yaml
oc create -f role.yaml
oc create -f rb.yaml
```

Run locally:
```
bin/manager -kubeconfig ~/path/to/kubeconfig
```
