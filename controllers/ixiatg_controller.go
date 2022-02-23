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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/utils/pointer"

	corev1 "k8s.io/api/core/v1"
	errapi "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	networkv1alpha1 "gitlab.it.keysight.com/athena/operator/api/v1alpha1"

	log "github.com/sirupsen/logrus"

	topopb "github.com/google/kne/proto/topo"

	"gopkg.in/yaml.v2"
)

const (
	CURRENT_NAMESPACE string = "ixiatg-op-system"
	SECRET_NAME       string = "ixia-pull-secret"

	SERVER_URL        string = "https://github.com/open-traffic-generator/ixia-c/releases/download/v"
	SERVER_LATEST_URL string = "https://github.com/open-traffic-generator/ixia-c/releases/latest/download"
	RELEASE_FILE      string = "/ixia-configmap.yaml"

	CONTROLLER_NAME      string = "ixia-c"
	CONFIG_MAP_NAME      string = "ixiatg-release-config"
	CONFIG_MAP_NAMESPACE string = "ixiatg-op-system"
	DEFAULT_VERSION      string = "latest"

	DS_RESTAPI         string = "Rest API"
	DS_CONFIGMAP       string = "Config Map"
	CONTROLLER_SERVICE string = "ixia-c-service"
	GRPC_SERVICE       string = "grpc-service"
	GNMI_SERVICE       string = "gnmi-service"
)

var (
	componentDep  map[string]topoDep = make(map[string]topoDep)
	latestVersion string             = ""
)

// IxiaTGReconciler reconciles a IxiaTG object
type IxiaTGReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type componentRel struct {
	Args    []string               `json:"args"`
	Command []string               `json:"command"`
	Env     map[string]interface{} `json:"env"`
	Name    string                 `json:"name"`
	Path    string                 `json:"path"`
	Tag     string                 `json:"tag"`
}

type Node struct {
	Name       string
	Containers map[string]componentRel
}

type topoDep struct {
	Source     string
	Controller Node
	Ixia       Node
}

type pubRel struct {
	Release string         `json:"release"`
	Images  []componentRel `json:"images"`
}

type pubReleases struct {
	Releases []pubRel `json:"releases"`
}

type ixiaConfigMap struct {
	ApiVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	MetaData   map[string]string `yaml:"metadata"`
	Data       struct {
		Versions string `yaml:"versions"`
	} `yaml:"data"`
}

//+kubebuilder:rbac:groups=network.keysight.com,resources=ixiatgs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.keysight.com,resources=ixiatgs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.keysight.com,resources=ixiatgs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Modify the Reconcile function to compare the state specified by
// the IxiaTG object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *IxiaTGReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("ixiatg", req.NamespacedName)

	ixia := &networkv1alpha1.IxiaTG{}
	err := r.Get(ctx, req.NamespacedName, ixia)

	log.Infof("Reconcile: %v, %s", ixia.Name, ixia.Namespace)

	if err != nil {
		if errapi.IsNotFound(err) {
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
			err = r.deleteController(ctx, ixia)
			if err != nil {
				log.Errorf("Failed to delete controller pod in %v, err %v", ixia.Namespace, err)
				return ctrl.Result{}, err
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

	requeue := false
	found := &corev1.Pod{}
	err = r.Get(ctx, types.NamespacedName{Name: ixia.Name, Namespace: ixia.Namespace}, found)
	if err != nil && errapi.IsNotFound(err) {
		// need to deploy, but first deploy controller if not present
		if err = r.deployController(ctx, ixia, secret); err == nil {
			log.Infof("Successfully deployed controller pod")
			if pod, err := r.podForIxia(ixia, secret); err == nil {
				//log.Infof("Creating pod %v", pod)
				err = r.Create(ctx, pod)
			}
		}

		if err == nil {
			log.Infof("Created!")
			requeue = true
		} else {
			log.Errorf("Failed to create pod for %v in %v - %v", ixia.Name, ixia.Namespace, err)
		}
	} else if err != nil {
		// Log but don't update status
		log.Error(err, "Failed to get pod")
		requeue = true
		err = nil
	} else {
		contPod := &corev1.Pod{}
		err = r.Get(ctx, types.NamespacedName{Name: CONTROLLER_NAME, Namespace: ixia.Namespace}, contPod)
		if err == nil {
			if found.Status.Phase == corev1.PodFailed {
				err = errors.New(fmt.Sprintf("Pod %s failed - %s", found.Name, found.Status.Reason))
			} else if contPod.Status.Phase == corev1.PodFailed {
				err = errors.New(fmt.Sprintf("Pod %s failed - %s", contPod.Name, contPod.Status.Reason))
			} else if found.Status.Phase != corev1.PodRunning || contPod.Status.Phase != corev1.PodRunning {
				requeue = true
				var contStatus []corev1.ContainerStatus
				for _, s := range found.Status.ContainerStatuses {
					contStatus = append(contStatus, s)
				}
				for _, s := range contPod.Status.ContainerStatuses {
					contStatus = append(contStatus, s)
				}
				for _, c := range contStatus {
					if c.State.Waiting != nil && c.State.Waiting.Reason == "ErrImagePull" {
						err = errors.New(fmt.Sprintf("Container %s failed - %s", c.Name, c.State.Waiting.Message))
						break
					}
				}
			}
		}
	}

	if !requeue || err != nil {
		if err != nil {
			ixia.Status.Status = "Failed"
			ixia.Status.Reason = err.Error()
			// Ensure this is an end state, no need to requeue
			requeue = false
		} else {
			ixia.Status.Status = "Success"
		}

		err = r.Status().Update(ctx, ixia)
		if err != nil {
			log.Errorf("Failed to update ixia status - %v", err)
			requeue = true
		}
	}

	if requeue {
		return ctrl.Result{RequeueAfter: time.Second}, err
	}
	return ctrl.Result{}, err
}

func (r *IxiaTGReconciler) getRelInfo(ctx context.Context, release string) error {
	var data []byte
	var err error
	source := DS_RESTAPI

	// First try downloading release dependency yaml file
	url := SERVER_URL + release + RELEASE_FILE
	if release == DEFAULT_VERSION {
		url = SERVER_LATEST_URL + RELEASE_FILE
	}
	log.Infof("Contacting Ixia server for release dependency info - %s", url)

	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(url)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			yamlData, err := ioutil.ReadAll(resp.Body)
			var yamlCfg ixiaConfigMap
			err = yaml.Unmarshal([]byte(yamlData), &yamlCfg)
			if err != nil {
				log.Errorf("Failed to parse downloaded release config file - %v", err)
			} else {
				data = []byte(yamlCfg.Data.Versions)
			}
		} else {
			err = errors.New(fmt.Sprintf("Got http response %v", resp.StatusCode))
			log.Errorf("Failed to download release config file - %v", err)
		}
	} else {
		log.Errorf("Failed to download release config file - %v", err)
	}

	if err != nil || len(data) == 0 {
		if release == DEFAULT_VERSION {
			log.Infof("Could not retrieve latest release information")
			return nil
		}

		log.Infof("Try locating in ConfigMap...")
		cfgData := &corev1.ConfigMap{}
		nsName := types.NamespacedName{Name: CONFIG_MAP_NAME, Namespace: CONFIG_MAP_NAMESPACE}
		source = DS_CONFIGMAP
		if err = r.Get(ctx, nsName, cfgData); err != nil {
			log.Infof("Failed to read ConfigMap - %v", err)
		} else {
			confData := cfgData.Data["versions"]
			data = []byte(confData)
		}
	}

	if len(data) == 0 {
		if _, ok := componentDep[release]; !ok {
			log.Infof("Version specific information could not be located; ensure a valid version is used")
			log.Infof("Also ensure the version specific ConfigMap yaml is applied if working in offline mode")
			return errors.New("Release dependency info could not be located")
		}

		return nil
	}

	return r.loadRelInfo(release, &data, false, source)
}

func (r *IxiaTGReconciler) loadRelInfo(release string, relData *[]byte, list bool, source string) error {
	var rel pubRel
	var relList pubReleases
	var err error
	initContSeq := 0

	if list {
		err = json.Unmarshal(*relData, &relList)
	} else {
		err = json.Unmarshal(*relData, &rel)
		relList = pubReleases{}
		relList.Releases = append(relList.Releases, rel)
	}
	if err != nil {
		log.Error(err, "Failed to unmarshall release dependency json")
		return err
	}

	for _, relEntry := range relList.Releases {
		if len(relEntry.Release) == 0 {
			continue
		}

		topoEntry := topoDep{Source: source, Controller: Node{Name: CONTROLLER_NAME, Containers: make(map[string]componentRel)}, Ixia: Node{Containers: make(map[string]componentRel)}}
		for _, image := range relEntry.Images {
			switch image.Name {
			case "controller":
				fallthrough
			case "gnmi-server":
				fallthrough
			case "grpc-server":
				topoEntry.Controller.Containers[image.Name] = image
			case "traffic-engine":
				fallthrough
			case "protocol-engine":
				topoEntry.Ixia.Containers[image.Name] = image
			default:
				if strings.HasPrefix(image.Name, "init-") {
					initContSeq = initContSeq + 1
					contName := fmt.Sprintf("init%d", initContSeq)
					topoEntry.Ixia.Containers[contName] = image
				} else {
					log.Errorf("Error unknown image name: %s", image.Name)
					return errors.New("Unknown image name")
				}
			}
		}

		componentDep[relEntry.Release] = topoEntry
		if release == DEFAULT_VERSION {
			latestVersion = relEntry.Release
			release = latestVersion
		}
		log.Infof("Found version info for %s through %s", relEntry.Release, source)
		log.Infof("Mapped controller components:")
		for _, val := range topoEntry.Controller.Containers {
			log.Infof("Name: %s, Image %s:%s, Args: %s, Command: %s, Env: %s",
				val.Name, val.Path, val.Tag, val.Args, val.Command, val.Env)
		}
		log.Infof("Mapped ixiatg node components:")
		for _, val := range topoEntry.Ixia.Containers {
			log.Infof("Name: %s, Image %s:%s, Args: %s, Command: %s, Env: %s",
				val.Name, val.Path, val.Tag, val.Args, val.Command, val.Env)
		}
	}

	if _, ok := componentDep[release]; !ok {
		log.Errorf("Release %s related dependency could not be located", release)
		return errors.New("Release dependency info could not be located")
	}

	return nil
}

func (r *IxiaTGReconciler) deleteController(ctx context.Context, ixia *networkv1alpha1.IxiaTG) error {
	found := &corev1.Pod{}
	if r.Get(ctx, types.NamespacedName{Name: CONTROLLER_NAME, Namespace: ixia.Namespace}, found) == nil {
		if err := r.Delete(ctx, found); err != nil {
			log.Errorf("Failed to delete associated controller %v - %v", found, err)
			return err
		}
		log.Infof("Deleted controller %v", found)
	}

	// Now delete the services
	service := &corev1.Service{}
	if r.Get(ctx, types.NamespacedName{Name: CONTROLLER_SERVICE, Namespace: ixia.Namespace}, service) == nil {
		if err := r.Delete(ctx, service); err != nil {
			log.Errorf("Failed to delete controller service %v - %v", service, err)
			return err
		}
		log.Infof("Deleted controller service %v", service)
	}

	if r.Get(ctx, types.NamespacedName{Name: GRPC_SERVICE, Namespace: ixia.Namespace}, service) == nil {
		if err := r.Delete(ctx, service); err != nil {
			log.Errorf("Failed to delete gRPC service %v - %v", service, err)
			return err
		}
		log.Infof("Deleted gRPC service %v", service)
	}

	if r.Get(ctx, types.NamespacedName{Name: GNMI_SERVICE, Namespace: ixia.Namespace}, service) == nil {
		if err := r.Delete(ctx, service); err != nil {
			log.Errorf("Failed to delete gNMI service %v - %v", service, err)
			return err
		}
		log.Infof("Deleted gNMI service %v", service)
	}

	return nil
}

func (r *IxiaTGReconciler) deployController(ctx context.Context, ixia *networkv1alpha1.IxiaTG, secret *corev1.Secret) error {
	podList := &corev1.PodList{}
	opts := []client.ListOption{
		client.InNamespace(ixia.Namespace),
	}
	var err error
	if ixia.Name == CONTROLLER_NAME {
		err = errors.New(fmt.Sprintf("Error: %s name is reserved for Controller pod, use some other name", CONTROLLER_NAME))
		log.Error(err, "Failed to create pod")
		return err
	}

	// First check if we have the component dependency data for the release
	depVersion := DEFAULT_VERSION
	if ixia.Spec.Version != "" {
		depVersion = ixia.Spec.Version
	} else {
		log.Infof("No ixiatg version specified, using default version %s", depVersion)
	}

	if _, ok := componentDep[depVersion]; !ok || depVersion == DEFAULT_VERSION || componentDep[depVersion].Source == DS_CONFIGMAP {
		if err := r.getRelInfo(ctx, depVersion); err != nil {
			log.Errorf("Failed to get release information for %s", depVersion)
			return err
		}
	}
	if depVersion == DEFAULT_VERSION {
		if latestVersion == "" {
			log.Errorf("Failed to get release information for %s", depVersion)
			return errors.New("Failed to get release information")
		} else {
			depVersion = latestVersion
		}
	}

	if err = r.List(ctx, podList, opts...); err != nil {
		log.Errorf("Failed to list current pods %v", err)
		return err
	}
	for _, p := range podList.Items {
		if p.ObjectMeta.Name == CONTROLLER_NAME {
			log.Infof("Controller already deployed for %s", ixia.Name)
			for _, c := range p.Spec.Containers {
				if c.Name == CONTROLLER_NAME {
					expVersion := componentDep[depVersion].Controller.Containers["controller"].Tag
					if strings.HasSuffix(c.Image, ":"+expVersion) {
						return nil
					}
					contVersion := strings.Split(c.Image, ":")
					err = errors.New(fmt.Sprintf("Version mismatch - expected %s, found %s", contVersion[1], depVersion))
					return err
				}
			}
		}
	}

	// Deploy controller and services
	imagePullSecrets := getImgPullSctSecret(secret)
	containers, err := r.containersForController(ixia, depVersion)
	if err != nil {
		return err
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CONTROLLER_NAME,
			Namespace: ixia.Namespace,
			Labels: map[string]string{
				"app": CONTROLLER_NAME,
			},
		},
		Spec: corev1.PodSpec{
			Containers:                    containers,
			ImagePullSecrets:              imagePullSecrets,
			TerminationGracePeriodSeconds: pointer.Int64(5),
		},
	}
	log.Infof("Creating controller pod %v", pod)
	err = r.Create(ctx, pod)
	if err != nil {
		log.Errorf("Failed to create pod %v in %v, err %v", pod.Name, pod.Namespace, err)
		return err
	}

	// Now create and map services
	services := r.getControllerService(ixia)
	for _, s := range services {
		err = r.Create(ctx, &s)
		if err != nil {
			log.Errorf("Failed to create service %v in %v, err %v", s, ixia.Namespace, err)
			return err
		}
	}

	return nil
}

func (r *IxiaTGReconciler) podForIxia(ixia *networkv1alpha1.IxiaTG, secret *corev1.Secret) (*corev1.Pod, error) {
	initContainers := []corev1.Container{}
	versionToDeploy := latestVersion
	if ixia.Spec.Version != "" && ixia.Spec.Version != DEFAULT_VERSION {
		versionToDeploy = ixia.Spec.Version
	}
	contPodMap := componentDep[versionToDeploy].Ixia.Containers
	args := []string{"2", "10"}
	for _, cont := range contPodMap {
		if strings.HasPrefix(cont.Name, "init-") {
			initCont := corev1.Container{
				Name:            cont.Name,
				Image:           cont.Path + ":" + cont.Tag,
				ImagePullPolicy: "IfNotPresent",
			}
			log.Infof("Adding init container image: %s", initCont.Image)
			initCont = updateControllerContainer(initCont, cont, args, nil)
			initContainers = append(initContainers, initCont)
		}
	}
	if len(initContainers) == 0 {
		log.Infof("Adding default init container")
		defaultInitCont := corev1.Container{
			Name:            "init-container",
			Image:           "networkop/init-wait:latest",
			Args:            args,
			ImagePullPolicy: "IfNotPresent",
		}
		initContainers = append(initContainers, defaultInitCont)
	}
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
			InitContainers:                initContainers,
			Containers:                    r.containersForIxia(ixia),
			ImagePullSecrets:              imagePullSecrets,
			TerminationGracePeriodSeconds: pointer.Int64(5),
		},
	}
	return pod, nil
}

func (r *IxiaTGReconciler) getControllerService(ixia *networkv1alpha1.IxiaTG) []corev1.Service {
	var services []corev1.Service

	// Controller service
	contPort := corev1.ServicePort{Name: "https", Port: 443, TargetPort: intstr.IntOrString{IntVal: 443}}
	services = append(services, corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CONTROLLER_SERVICE,
			Namespace: ixia.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": CONTROLLER_NAME,
			},
			Ports: []corev1.ServicePort{contPort},
			Type:  "LoadBalancer",
		},
	})

	// gRPC service
	contPort = corev1.ServicePort{Name: "grpc", Port: 40051, TargetPort: intstr.IntOrString{IntVal: 40051}}
	services = append(services, corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GRPC_SERVICE,
			Namespace: ixia.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": CONTROLLER_NAME,
			},
			Ports: []corev1.ServicePort{contPort},
			Type:  "LoadBalancer",
		},
	})

	// gNMI service
	contPort = corev1.ServicePort{Name: "gnmi", Port: 50051, TargetPort: intstr.IntOrString{IntVal: 50051}}
	services = append(services, corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GNMI_SERVICE,
			Namespace: ixia.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": CONTROLLER_NAME,
			},
			Ports: []corev1.ServicePort{contPort},
			Type:  "LoadBalancer",
		},
	})

	return services
}

func updateControllerContainer(cont corev1.Container, pubRel componentRel, args []string, cmd []string) corev1.Container {
	cfgMapEnv := make(map[string]string)
	for ek, ev := range pubRel.Env {
		cfgMapEnv[ek] = ev.(string)
	}
	conEnv := getEnvData(nil, cfgMapEnv, nil)
	if len(pubRel.Args) > 0 {
		args = pubRel.Args
	}
	if len(pubRel.Command) > 0 {
		cmd = pubRel.Command
	}

	if len(args) > 0 {
		cont.Args = args
	}
	if len(cmd) > 0 {
		cont.Command = cmd
	}
	if len(conEnv) > 0 {
		cont.Env = conEnv
	}
	return cont
}

func (r *IxiaTGReconciler) containersForController(ixia *networkv1alpha1.IxiaTG, release string) ([]corev1.Container, error) {
	log.Infof("Inside containersForController: %s", ixia.Name)
	contPodMap := componentDep[release].Controller.Containers
	var containers []corev1.Container

	// Adding controller container
	if pubRel, ok := contPodMap["controller"]; ok {
		log.Infof("Deploying Controller version %s for config version %s, ns %s (source %s)", pubRel.Tag, release, ixia.Namespace, componentDep[release].Source)

		name := CONTROLLER_NAME
		image := pubRel.Path + ":" + pubRel.Tag
		args := []string{"--accept-eula", "--debug"}
		command := []string{}
		log.Infof("Adding Pod: %s, Container: %s, Image: %s", CONTROLLER_NAME, name, image)
		container := updateControllerContainer(corev1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: "IfNotPresent",
		}, pubRel, args, command)
		containers = append(containers, container)
	} else {
		return nil, errors.New(fmt.Sprintf("Controller image data not found for release %s", release))
	}

	// Adding grpc container
	if pubRel, ok := contPodMap["grpc-server"]; ok {
		log.Infof("Deploying gRPC version %s for config version %s, ns %s (source %s)", pubRel.Tag, release, ixia.Namespace, componentDep[release].Source)

		name := "grpc"
		image := pubRel.Path + ":" + pubRel.Tag
		args := []string{}
		command := []string{"python3", "-m", "grpc_server", "--app-mode", "athena", "--target-host", CONTROLLER_SERVICE, "--target-port", "443", "--log-stdout", "--log-debug"}
		var ports []corev1.ContainerPort
		ports = append(ports, corev1.ContainerPort{Name: "grpc", ContainerPort: 40051, Protocol: "TCP"})
		log.Infof("Adding Pod: %s, Container: %s, Image: %s", CONTROLLER_NAME, name, image)
		container := updateControllerContainer(corev1.Container{
			Name:            name,
			Image:           image,
			Ports:           ports,
			ImagePullPolicy: "IfNotPresent",
		}, pubRel, args, command)
		containers = append(containers, container)
	} else {
		return nil, errors.New(fmt.Sprintf("gRPC image data not found for release %s", release))
	}

	// Adding gnmi container
	if pubRel, ok := contPodMap["gnmi-server"]; ok {
		log.Infof("Deploying gNMI version %s for config version %s, ns %s (source %s)", pubRel.Tag, release, ixia.Namespace, componentDep[release].Source)

		name := "gnmi"
		image := pubRel.Path + ":" + pubRel.Tag
		args := []string{}
		command := []string{"python3", "-m", "otg_gnmi", "--server-port", "50051", "--app-mode", "athena", "--target-host", CONTROLLER_SERVICE, "--target-port", "443", "--insecure"}
		var ports []corev1.ContainerPort
		ports = append(ports, corev1.ContainerPort{Name: "gnmi", ContainerPort: 50051, Protocol: "TCP"})
		log.Infof("Adding Pod: %s, Container: %s, Image: %s", CONTROLLER_NAME, name, image)
		container := updateControllerContainer(corev1.Container{
			Name:            name,
			Image:           image,
			Ports:           ports,
			ImagePullPolicy: "IfNotPresent",
		}, pubRel, args, command)
		containers = append(containers, container)
	} else {
		return nil, errors.New(fmt.Sprintf("gRPC image data not found for release %s", release))
	}

	log.Infof("Done containersForIxia() total containers %v!", len(containers))
	return containers, nil
}

func (r *IxiaTGReconciler) containersForIxia(ixia *networkv1alpha1.IxiaTG) []corev1.Container {
	log.Infof("Inside containersForIxia: %v", ixia.Spec.Config)

	conEnvs := map[string]string{
		"OPT_LISTEN_PORT":  "5555",
		"ARG_CORE_LIST":    "2 3 4",
		"ARG_IFACE_LIST":   "virtual@af_packet,eth1",
		"OPT_NO_HUGEPAGES": "Yes",
	}
	conSecurityCtx := getDefaultSecurityContext()
	var containers []corev1.Container
	config := &topopb.Config{}

	if ixia.Spec.Config != "" {
		err := json.Unmarshal([]byte(ixia.Spec.Config), config)
		if err != nil {
			log.Infof("config unmarshalling failed: %v", err)
		}
	}

	versionToDeploy := latestVersion
	if ixia.Spec.Version != "" && ixia.Spec.Version != DEFAULT_VERSION {
		versionToDeploy = ixia.Spec.Version
	}
	for k, v := range componentDep[versionToDeploy].Ixia.Containers {
		if strings.HasPrefix(v.Name, "init-") {
			continue
		}
		log.Infof("Deploying %s version %s for config version %s, ns %s (source %s)", k, v.Tag, versionToDeploy, ixia.Namespace, componentDep[versionToDeploy].Source)

		name := ixia.Name + "-" + k
		image := v.Path + ":" + v.Tag
		cfgMapEnv := make(map[string]string)
		log.Infof("Adding Pod: %s, Container: %s, Image: %s", ixia.Name, name, image)
		container := corev1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: conSecurityCtx,
		}
		conArgs := append(v.Args, config.Args...)
		if len(conArgs) > 0 {
			container.Args = conArgs
		}
		if len(v.Command) > 0 {
			container.Command = v.Command
		}
		for ek, ev := range v.Env {
			cfgMapEnv[ek] = ev.(string)
		}
		conEnv := getEnvData(conEnvs, cfgMapEnv, config.Env)
		if len(conEnv) > 0 {
			container.Env = conEnv
		}
		containers = append(containers, container)
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

func updateEnvData(data map[string]string, update map[string]string) map[string]string {
	if len(update) > 0 {
		for k, v := range update {
			data[k] = v
		}
	}
	return data
}

func getEnvData(defEnv map[string]string, cfgMapEnv map[string]string, cfgEnv map[string]string) []corev1.EnvVar {
	envData := make(map[string]string)
	conEnvs := []corev1.EnvVar{}

	envData = updateEnvData(envData, defEnv)
	envData = updateEnvData(envData, cfgMapEnv)
	envData = updateEnvData(envData, cfgEnv)

	for key, value := range envData {
		conEnvs = append(conEnvs, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}
	return conEnvs
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
		if errapi.IsNotFound(err) {
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
			if err != nil && errapi.IsNotFound(err) {
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
	ips = append(ips, corev1.LocalObjectReference{Name: string(SECRET_NAME)})
	return ips
}
