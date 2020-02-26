SELinux policy helper operator
==============================

This is a controller that listens to all pods in the system.

It will attempt to generate a policy for pods when the pod is annotated with
a specific tag "generate-selinux-policy" and the pod is in a running
state.

It is also possible to annotate a namespace itself, this will try to generate a
policy for each pod running on it.

In order to generate the policy, it spawns a pod with the selinux-k8s
tool which uses udica to generate the policy. It will spit out a
configmap with the appropriate policy.

The link to the tool is the following:

https://github.com/JAORMX/selinux-k8s

Note that this now requires the selinux-operator to be installed:

https://github.com/JAORMX/selinux-operator

Instructions
------------

Install the selinux-operator with the instructions from that repository.

Create assets in cluster:

```
oc create -f deploy/
```

Demo
----

[![asciicast](https://asciinema.org/a/295362.svg)](https://asciinema.org/a/295362)

TODO
----

- [ ] Allow passing extra capabilities as annotations.
