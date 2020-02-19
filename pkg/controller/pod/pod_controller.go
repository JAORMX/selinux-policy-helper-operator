package pod

import (
	"context"
	hash "crypto/sha1"
	"fmt"
	"io"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	trueVal           = true
	hostPathDir       = corev1.HostPathDirectory
	hostPathFile      = corev1.HostPathFile
	operatorNamespace = "openshift-selinux-policy-helper-operator"
)

type updatePredicate struct {
	predicate.Funcs
}

func (updatePredicate) Create(e event.CreateEvent) bool {
	return false
}

func (updatePredicate) Delete(e event.DeleteEvent) bool {
	return false
}
func (updatePredicate) Generic(e event.GenericEvent) bool {
	return false
}

var log = logf.Log.WithName("controller_pod")

// Add creates a new Pod Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePod{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("pod-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for updates to Pod resource
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}, &updatePredicate{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcilePod implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcilePod{}

// ReconcilePod reconciles a Pod object
type ReconcilePod struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Pod object and makes changes based on the state read
// and what is in the Pod.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePod) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	pod := &corev1.Pod{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, pod); err != nil {
		return reconcile.Result{}, ignoreNotFound(err)
	}

	if isPolicyHelperPod(pod) {
		reqLogger.Info("Reconciling Pod")
		return r.handlePolicyHelperPod(pod, reqLogger)
	}

	ns := &corev1.Namespace{}
	podNS := types.NamespacedName{Name: pod.Namespace}
	if err := r.client.Get(context.TODO(), podNS, ns); err != nil {
		// If there's no Namespace existing in the cluster... we don't bother trying
		//with this pod. It'll be deleted anyway.
		return reconcile.Result{}, ignoreNotFound(err)
	}

	if r.shouldPodBeSkipped(pod, ns) {
		return reconcile.Result{}, nil
	}

	return r.handlePodThatNeedsPolicy(pod, reqLogger)
}

func (r *ReconcilePod) handlePolicyHelperPod(pod *corev1.Pod, log logr.Logger) (reconcile.Result, error) {
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		if err := r.client.Delete(context.TODO(), pod); err != nil {
			log.Error(err, "Could not delete policy helper pod", "pod name", pod.Name)
			return reconcile.Result{}, ignoreNotFound(err)
		}
	case corev1.PodFailed:
		log.Info("Policy helper pod failed. Please check the logs to see why", "pod name", pod.Name)
	default:
		log.Info("Skipping policy helper pod since it's not done", "pod name", pod.Name)
	}
	return reconcile.Result{}, nil
}

func (r *ReconcilePod) handlePodThatNeedsPolicy(pod *corev1.Pod, log logr.Logger) (reconcile.Result, error) {
	// Print the pod
	log.Info("Running policy helper for pod", "pod name", pod.Name)

	// Run policy helper
	selinuxPodNSName := types.NamespacedName{Name: getSelinuxPodName(pod.Name, pod.Namespace), Namespace: operatorNamespace}
	selinuxPod := &corev1.Pod{}
	if err := r.client.Get(context.TODO(), selinuxPodNSName, selinuxPod); err != nil {
		if errors.IsNotFound(err) {
			selinuxPod = newSelinuxPolicyMakerPod(pod.Name, pod.Namespace, pod.Spec.NodeName)
			if err := r.client.Create(context.TODO(), selinuxPod); err != nil {
				log.Error(err, "Could not write selinux Pod")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, err
	}

	podCopy := pod.DeepCopy()
	// update pod annotation
	podCopy.Annotations["selinux-policy"] = selinuxPodNSName.String()
	if err := r.client.Update(context.TODO(), podCopy); err != nil {
		log.Error(err, "Could not update target pod", "pod name", podCopy.Name)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *ReconcilePod) shouldPodBeSkipped(pod *corev1.Pod, ns *corev1.Namespace) bool {
	// If the namespace or pod doesn't have the relevant annotation, we can skip it
	_, nsHasAnnotation := ns.Annotations["generate-selinux-policy"]
	_, podHasAnnotation := pod.Annotations["generate-selinux-policy"]
	if !nsHasAnnotation && !podHasAnnotation {
		return true
	}

	// We only care for pods that are running
	if pod.Status.Phase != corev1.PodRunning {
		return true
	}

	// Have we processed this pod before?
	if _, hasAnnotation := pod.Annotations["selinux-policy"]; hasAnnotation {
		return true
	}

	return false
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

func getSelinuxPodName(targetPodName, targetPodNamespace string) string {
	hasher := hash.New()
	io.WriteString(hasher, targetPodName+"-"+targetPodNamespace)
	return "selinux-k8s-" + fmt.Sprintf("%x", hasher.Sum(nil))
}

func newSelinuxPolicyMakerPod(targetPodName, targetPodNamespace, targetNodeName string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSelinuxPodName(targetPodName, targetPodNamespace),
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
					Args:    []string{"--name", targetPodName, "--namespace", targetPodNamespace},
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

func ignoreNotFound(err error) error {
	if errors.IsNotFound(err) {
		return nil
	}
	// Error reading the object - requeue the request.
	return err
}
