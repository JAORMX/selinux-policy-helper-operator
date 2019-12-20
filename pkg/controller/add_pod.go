package controller

import (
	"github.com/JAORMX/selinux-policy-helper-operator/pkg/controller/pod"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, pod.Add)
}
