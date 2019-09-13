/*
Copyright 2019 Red Hat Inc..

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	trueVal           = true
	hostPathDir       = corev1.HostPathDirectory
	hostPathFile      = corev1.HostPathFile
	operatorNamespace = "selinux-policy-helper-operator"
)

// ReconcilePods reconciles Pods
type ReconcilePods struct {
	// client can be used to retrieve objects from the APIServer.
	Client client.Client
	Log    logr.Logger
}

// Implement reconcile.Reconciler so the controller can reconcile objects
var _ reconcile.Reconciler = &ReconcilePods{}

func (r *ReconcilePods) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// set up a convenient log object so we don't have to Type request over and over again
	log := r.Log.WithValues("request", request)

	// Fetch the Pod from the cache
	pod := &corev1.Pod{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, pod)
	if errors.IsNotFound(err) {
		log.Info("Could not find Pod. But it's ok.")
		return reconcile.Result{}, nil
	}
	if err != nil {
		log.Error(err, "Could not fetch Pod")
		return reconcile.Result{}, err
	}

	if isPolicyHelperPod(pod) {
		return r.handlePolicyHelperPod(request, pod, log)
	}

	if skip, message := shouldPodBeSkipped(pod); skip {
		log.Info(message, "pod name", pod.Name)
		return reconcile.Result{}, nil
	}

	return r.handlePodThatNeedsPolicy(request, pod, log)
}

func (r *ReconcilePods) handlePolicyHelperPod(request reconcile.Request, pod *corev1.Pod, log logr.Logger) (reconcile.Result, error) {
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		if err := r.Client.Delete(context.TODO(), pod); err != nil {
			log.Error(err, "Could not delete policy helper pod", "pod name", pod.Name)
			return reconcile.Result{}, err
		}
	case corev1.PodFailed:
		log.Error(nil, "Policy helper pod failed. Please check the logs to see why", "pod name", pod.Name)
	default:
		log.Info("Skipping policy helper pod since it's not done", "pod name", pod.Name)
	}
	return reconcile.Result{}, nil
}

func (r *ReconcilePods) handlePodThatNeedsPolicy(request reconcile.Request, pod *corev1.Pod, log logr.Logger) (reconcile.Result, error) {
	// Print the pod
	log.Info("Running policy helper for pod", "pod name", pod.Name)

	// Run policy helper
	selinuxPodNSName := types.NamespacedName{Name: getSelinuxPodName(request.Name), Namespace: operatorNamespace}
	selinuxPod := newSelinuxPolicyMakerPod(request.Name, pod.Spec.NodeName)
	err := r.Client.Create(context.TODO(), selinuxPod)
	if err != nil {
		log.Error(err, "Could not write selinux Pod")
		return reconcile.Result{}, err
	}

	// update pod annotation
	pod.Annotations["selinux-policy"] = selinuxPodNSName.String()
	err = r.Client.Update(context.TODO(), pod)
	if err != nil {
		log.Error(err, "Could not update target pod", "pod name", pod.Name)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func shouldPodBeSkipped(pod *corev1.Pod) (bool, string) {
	if pod.Annotations == nil {
		return true, "Skipping pod without annotations"
	}

	// If the pod doesn't have the relevant annotation, we can skip it
	if _, hasAnnotation := pod.Annotations["generate-selinux-policy"]; !hasAnnotation {
		return true, "Skipping pod without selinux annotations"
	}

	// We only care for pods that are running
	if pod.Status.Phase != corev1.PodRunning {
		return true, "Skipping pod that's not running"
	}

	// Have we processed this pod before?
	if _, hasAnnotation := pod.Annotations["selinux-policy"]; hasAnnotation {
		return true, "Skipping pod that has already been processed"
	}

	return false, ""
}

func isPolicyHelperPod(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}

	if _, hasAnnotation := pod.Annotations["owned-by-selinux-policy-helper"]; hasAnnotation {
		return true
	}
	return false
}

func getSelinuxPodName(targetPodName string) string {
	return "selinux-k8s-for-" + targetPodName
}

func newSelinuxPolicyMakerPod(targetPodName, targetNodeName string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSelinuxPodName(targetPodName),
			Namespace: operatorNamespace,
			Annotations: map[string]string{
				"owned-by-selinux-policy-helper": "",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "selinux-k8s",
					Image:   "quay.io/jaosorior/selinux-k8s:latest",
					Command: []string{"selinuxk8s"},
					Args:    []string{"--name", targetPodName},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &trueVal,
					},
					VolumeMounts: []corev1.VolumeMount{
						corev1.VolumeMount{
							Name:      "fsselinux",
							MountPath: "/sys/fs/selinux",
						},
						corev1.VolumeMount{
							Name:      "etcselinux",
							MountPath: "/etc/selinux",
						},
						corev1.VolumeMount{
							Name:      "varlibselinux",
							MountPath: "/var/lib/selinux",
						},
						corev1.VolumeMount{
							Name:      "varruncrio",
							MountPath: "/var/run/crio",
						},
						corev1.VolumeMount{
							Name:      "crictlyaml",
							MountPath: "/etc/crictl.yaml",
						},
					},
				},
			},
			NodeName:           targetNodeName,
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: "selinux-policy-helper-operator",
			Volumes: []corev1.Volume{
				corev1.Volume{
					Name: "fsselinux",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/sys/fs/selinux",
							Type: &hostPathDir,
						},
					},
				},
				corev1.Volume{
					Name: "etcselinux",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/etc/selinux",
							Type: &hostPathDir,
						},
					},
				},
				corev1.Volume{
					Name: "varlibselinux",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/lib/selinux",
							Type: &hostPathDir,
						},
					},
				},
				corev1.Volume{
					Name: "varruncrio",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/run/crio",
							Type: &hostPathDir,
						},
					},
				},
				corev1.Volume{
					Name: "crictlyaml",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/etc/crictl.yaml",
							Type: &hostPathFile,
						},
					},
				},
			},
		},
	}
}
