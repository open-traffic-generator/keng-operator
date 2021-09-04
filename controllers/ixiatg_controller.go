/*
Copyright 2021.

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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	networkv1alpha1 "github.com/lemoncrust/iris/api/v1alpha1"

	log "github.com/sirupsen/logrus"

	topopb "github.com/google/kne/proto/topo"
)

var CURRENT_NAMESPACE string = "ixiatg-op-system"
var SECRET_NAME string = "ixia-pull-secret"

// IxiaTGReconciler reconciles a IxiaTG object
type IxiaTGReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=network.keysight.com,resources=ixiatgs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.keysight.com,resources=ixiatgs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.keysight.com,resources=ixiatgs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the IxiaTG object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *IxiaTGReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("ixiatg", req.NamespacedName)

	// your logic here
	ixia := &networkv1alpha1.IxiaTG{}
	err := r.Get(ctx, req.NamespacedName, ixia)

	log.Infof("INSIDE Reconcile: %v, %v", ixia, err)

	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Infof("Ixia resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get Ixia")
		return ctrl.Result{}, err
	}

	// Check if we need to creatte resources or not
	secret, err := r.ReconcileSecret(ctx, req, ixia)

	myFinalizerName := "keysight.com/finalizer"

	if ixia.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Infof("Checking for finalizer")
		if !containsString(ixia.GetFinalizers(), myFinalizerName) {
			log.Infof("Adding finalizer")
			controllerutil.AddFinalizer(ixia, myFinalizerName)
			if err := r.Update(ctx, ixia); err != nil {
				log.Error(err)
				return ctrl.Result{}, err
			}
			log.Infof("Added finalizer")
		}
	} else {
		if containsString(ixia.GetFinalizers(), myFinalizerName) {
			found := &corev1.Pod{}
			if r.Get(ctx, types.NamespacedName{Name: ixia.Name, Namespace: ixia.Namespace}, found) == nil {
				if err := r.Delete(ctx, found); err != nil {
					log.Errorf("Failed to delete associated pod %v %v", found, err)
					return ctrl.Result{}, err
				}
				log.Infof("Deleted pod %v", found)
			}

			controllerutil.RemoveFinalizer(ixia, myFinalizerName)
			if err := r.Update(ctx, ixia); err != nil {
				log.Errorf("Failed to delete finalizer, %v", err)
				return ctrl.Result{}, err
			}
			log.Infof("Deleted finalizer")
		}
		return ctrl.Result{}, nil
	}

	found := &corev1.Pod{}
	err = r.Get(ctx, types.NamespacedName{Name: ixia.Name, Namespace: ixia.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// need to deploy
		log.Infof("Creating pod")
		pod := r.podForIxia(ixia, secret)
		log.Infof("Creating pod %v", pod)
		err = r.Create(ctx, pod)
		if err != nil {
			log.Errorf("Failed to create pod %v in %v, err %v", pod.Name, pod.Namespace, err)
			return ctrl.Result{}, err
		}
		log.Infof("Created!")
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get pod")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *IxiaTGReconciler) podForIxia(ixia *networkv1alpha1.IxiaTG, secret *corev1.Secret) *corev1.Pod {

	imagePullSecrets := getImgPullSctSecret(secret)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ixia.Name,
			Namespace: ixia.Namespace,
			Labels: map[string]string{
				"app":  ixia.Name,
				"topo": ixia.Namespace,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  "init-container",
				Image: "networkop/init-wait:latest",
				Args: []string{
					"2",
					"10",
				},
				ImagePullPolicy: "IfNotPresent",
			}},
			Containers:       r.containersForIxia(ixia, secret),
			ImagePullSecrets: imagePullSecrets,
		},
	}
	return pod
}

func (r *IxiaTGReconciler) containersForIxia(ixia *networkv1alpha1.IxiaTG, secret *corev1.Secret) []corev1.Container {

	log.Infof("Inside containersForIxia: %v", ixia.Spec.Config)
	conImage := "gcr.io/kt-nts-athena-dev/athena/traffic-engine:1.2.0.8"
	conEnvs := getDefaultEnvVar()
	conSecurityCtx := getDefaultSecurityContext()
	var containers []corev1.Container

	config := &topopb.Config{}
	err := json.Unmarshal([]byte(ixia.Spec.Config), config)
	if err != nil {
		log.Infof("Unmarshalling failed, creating container with defaults. Error : %v", err)
		containers = []corev1.Container{{
			Name:            ixia.Name,
			Image:           conImage,
			Env:             conEnvs,
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: conSecurityCtx,
		}}
	} else {
		log.Infof("Unmarshalling successful, creating container with config data: %v", config)
		if len(config.Env) > 0 {
			conEnvs = updateEnvData(config.Env, conEnvs)
		}
		if len(config.Image) > 0 {
			images := strings.Split(config.Image, ",")
			for i, image := range images {
				name := ixia.Name
				if strings.Contains(image, "traffic") {
					name = name + "-dp"
				} else if strings.Contains(image, "protocol") {
					name = name + "-cp"
				} else {
					name = name + "-" + strconv.Itoa(i)
				}
				log.Infof("Adding Pod: %s, Container: %s, Image: %s",
					ixia.Name, name, image)
				containers = append(containers, corev1.Container{
					Name:            name,
					Image:           image,
					Env:             conEnvs,
					ImagePullPolicy: "IfNotPresent",
					SecurityContext: conSecurityCtx,
				})
			}
		}
	}
	log.Infof("Done containersForIxia() total containers %v!", len(containers))
	return containers
}

// SetupWithManager sets up the controller with the Manager.
func (r *IxiaTGReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1alpha1.IxiaTG{}).
		Complete(r)
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func updateEnvData(recvEnv map[string]string, conEnvs []corev1.EnvVar) []corev1.EnvVar {
	for key, value := range recvEnv {
		found := false
		for index, env := range conEnvs {
			if env.Name == key {
				found = true
				conEnvs[index].Value = value
			}
		}
		if !found {
			conEnvs = append(conEnvs, corev1.EnvVar{
				Name:  key,
				Value: value,
			})
		}
	}
	return conEnvs
}

func getDefaultEnvVar() []corev1.EnvVar {
	kv := map[string]string{
		"OPT_LISTEN_PORT":  "5555",
		"ARG_CORE_LIST":    "2 3 4",
		"ARG_IFACE_LIST":   "virtual@af_packet,eth1",
		"OPT_NO_HUGEPAGES": "Yes",
	}
	var envVar []corev1.EnvVar
	for k, v := range kv {
		envVar = append(envVar, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}
	return envVar
}

func getDefaultSecurityContext() *corev1.SecurityContext {
	var sc *corev1.SecurityContext = new(corev1.SecurityContext)
	t := true
	sc.Privileged = new(bool)
	sc.Privileged = &t
	return sc
}

func (r *IxiaTGReconciler) ReconcileSecret(ctx context.Context,
	req ctrl.Request, ixia *networkv1alpha1.IxiaTG) (*corev1.Secret, error) {
	_ = r.Log.WithValues("ixiatg", req.NamespacedName)
	// Fetch the Secret instance
	instance := &corev1.Secret{}

	//err := r.Get(ctx, req.NamespacedName, instance)
	currNamespace := new(types.NamespacedName)
	currNamespace.Namespace = CURRENT_NAMESPACE
	currNamespace.Name = SECRET_NAME
	log.Infof("Getting Secret for namespace : %v", *currNamespace)
	err := r.Get(ctx, *currNamespace, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Infof("No Secret found in namespace : %v", *currNamespace)
			return nil, nil
		}
		// Error reading the object - requeue the request.
		return nil, err
	}

	if rep, ok := instance.Annotations["secretsync.ixiatg.com/replicate"]; ok {
		if rep == "true" {
			targetSecret, err := createSecret(instance, currNamespace.Name, ixia.Namespace)
			if err != nil {
				return nil, err
			}
			secret := &corev1.Secret{}
			err = r.Get(ctx, types.NamespacedName{Name: targetSecret.Name, Namespace: targetSecret.Namespace}, secret)
			if err != nil && errors.IsNotFound(err) {
				log.Info(fmt.Sprintf("Target secret %s doesn't exist, creating it in namespace %s", targetSecret.Name, targetSecret.Namespace))
				err = r.Create(ctx, targetSecret)
				if err != nil {
					return nil, err
				}
			} else {
				log.Info(fmt.Sprintf("Target secret %s exists, updating it now in namespace %s", targetSecret.Name, targetSecret.Namespace))
				err = r.Update(ctx, targetSecret)
				if err != nil {
					return nil, err
				}
			}
			return targetSecret, err
		}
	}
	return nil, nil
}

func createSecret(secret *corev1.Secret, name string, namespace string) (*corev1.Secret, error) {
	labels := map[string]string{
		"secretsync.ixiatg.com/replicated-from": fmt.Sprintf("%s.%s", secret.Namespace, secret.Name),
	}
	annotations := map[string]string{
		"secretsync.ixiatg.com/replicated-time":             time.Now().Format("Mon Jan 2 15:04:05 MST 2006"),
		"secretsync.ixiatg.com/replicated-resource-version": secret.ResourceVersion,
	}
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: secret.TypeMeta.APIVersion,
			Kind:       secret.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: secret.Data,
		Type: secret.Type,
	}, nil
}

func getImgPullSctSecret(secret *corev1.Secret) []corev1.LocalObjectReference {
	var ips []corev1.LocalObjectReference
	//ips = append(ips, corev1.LocalObjectReference{Name: string(secret.Data[".dockerconfigjson"][:])})a
	ips = append(ips, corev1.LocalObjectReference{Name: string(SECRET_NAME)})
	return ips
}
