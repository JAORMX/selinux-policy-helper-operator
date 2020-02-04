SELinux policy helper operator
==============================

This is a controller that listens to all pods in the system.

It will attempt to generate a policy for pods when the pod is annotated with
a specific tag "generate-selinux-policy" and the pod is in a running
state.

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

[![asciicast](https://asciinema.org/a/RnjsiiQYRDiLcB8hbhKiIJF5B.svg)](https://asciinema.org/a/RnjsiiQYRDiLcB8hbhKiIJF5B)

TODO
----

- [ ] Allow passing extra capabilities as annotations.

- [ ] Allow monitoring only certain namespaces. This would enable the use-case
  of developer-only namespaces.

- [ ] Allow annotating full namespaces. This would enable folks to annotate the
  namespaces and get policies for all the pods running in them.
