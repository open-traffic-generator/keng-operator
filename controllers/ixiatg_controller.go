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
	"strconv"
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

type controllerMap struct {
	LocationMap []location `yaml:"location_map"`
}

type location struct {
	Location string `yaml:"location"`
	EndPoint string `yaml:"endpoint"`
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

	myFinalizerName := "keysight.com/finalizer"
	if ixia.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Infof("Checking for finalizer")
		if !containsString(ixia.GetFinalizers(), myFinalizerName) {
			log.Infof("Adding finalizer")
			controllerutil.AddFinalizer(ixia, myFinalizerName)
			if err = r.Update(ctx, ixia); err != nil {
				log.Error(err)
				return ctrl.Result{}, err
			}
			log.Infof("Added finalizer")
		}
	} else {
		if containsString(ixia.GetFinalizers(), myFinalizerName) {
			for _, intf := range ixia.Status.Interfaces {
				if err = r.deleteIxiaPod(ctx, intf.PodName, ixia); err != nil {
					//log.Errorf("Failed to delete associated pod %v %v", intf.PodName, err)
					return ctrl.Result{}, err
				}
			}
			err = r.deleteController(ctx, ixia)
			if err != nil {
				log.Errorf("Failed to delete controller pod in %v, err %v", ixia.Namespace, err)
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(ixia, myFinalizerName)
			if err = r.Update(ctx, ixia); err != nil {
				log.Errorf("Failed to delete finalizer, %v", err)
				return ctrl.Result{}, err
			}
			log.Infof("Deleted finalizer")
		}

		return ctrl.Result{}, nil
	}

	// First we verify if the desired state is already reached; otherwise handle accordingly
	if ixia.Spec.DesiredState == ixia.Status.State {
		return ctrl.Result{}, nil
	} else if ixia.Spec.DesiredState == "INITIATED" {
		genPodNames := []networkv1alpha1.IxiaTGIntfStatus{}
		for _, intf := range ixia.Spec.Interfaces {
			podName := ixia.Name + "-port-" + intf.Name
			if intf.Group != "" {
				podName = ixia.Name + "-port-group-" + intf.Group
			}
			genPodNames = append(genPodNames, networkv1alpha1.IxiaTGIntfStatus{PodName: podName, Name: intf.Name})
		}

		svcList := []string{}
		for name, _ := range ixia.Spec.ApiEndPoint {
			svcList = append(svcList, "service-"+name+"-"+ixia.Name+"-controller")
		}
		genSvcEP := networkv1alpha1.IxiaTGSvcEP{PodName: ixia.Name + "-controller", ServiceName: svcList}
		ixia.Status.Interfaces = genPodNames
		ixia.Status.State = ixia.Spec.DesiredState
		ixia.Status.ApiEndPoint = genSvcEP

		err = r.Status().Update(ctx, ixia)
		if err != nil {
			log.Errorf("Failed to update ixia status - %v", err)
			return ctrl.Result{RequeueAfter: time.Second}, err
		}

		return ctrl.Result{}, nil
	} else if ixia.Spec.DesiredState != "DEPLOYED" {
		err = errors.New(fmt.Sprintf("Unknown desired state found %s", ixia.Spec.DesiredState))
		log.Error(err)
		ixia.Status.Reason = err.Error()
		ixia.Status.State = "FAILED"

		return ctrl.Result{}, nil
	}

	// Check if we need to create resources or not
	secret, err := r.ReconcileSecret(ctx, req, ixia)

	requeue := false
	found := &corev1.Pod{}
	otgCtrlName := ixia.Name + "-controller"
	err = r.Get(ctx, types.NamespacedName{Name: otgCtrlName, Namespace: ixia.Namespace}, found)
	if err != nil && errapi.IsNotFound(err) {
		podMap := make(map[string][]string)
		for _, intf := range ixia.Status.Interfaces {
			if _, ok := podMap[intf.PodName]; ok {
				podMap[intf.PodName] = []string{intf.Name}
			} else {
				podMap[intf.PodName] = append(podMap[intf.PodName], intf.Name)
			}
		}
		// need to deploy, but first deploy controller if not present
		if err = r.deployController(ctx, &podMap, ixia, secret); err == nil {
			log.Infof("Successfully deployed controller pod")
			for name, intfs := range podMap {
				log.Infof("Creating pod %v", name)
				if err = r.podForIxia(ctx, name, intfs, ixia, secret); err != nil {
					//log.Infof("Pod %v create failed!", name)
					break
				}
				//log.Infof("Pod %v created!", name)
			}
		}

		if err == nil {
			log.Infof("All pods created!")
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
		var contStatus []corev1.ContainerStatus
		if found.Status.Phase == corev1.PodFailed {
			err = errors.New(fmt.Sprintf("Pod %s failed - %s", found.Name, found.Status.Reason))
		}
		if err == nil && found.Status.Phase != corev1.PodRunning {
			requeue = true
			for _, s := range found.Status.ContainerStatuses {
				contStatus = append(contStatus, s)
			}
		}
		if err == nil {
			for _, podEntry := range ixia.Status.Interfaces {
				err = r.Get(ctx, types.NamespacedName{Name: podEntry.PodName, Namespace: ixia.Namespace}, found)
				if err != nil {
					break
				}
				if found.Status.Phase == corev1.PodFailed {
					err = errors.New(fmt.Sprintf("Pod %s failed - %s", found.Name, found.Status.Reason))
					break
				}
				if found.Status.Phase != corev1.PodRunning {
					requeue = true
					for _, s := range found.Status.ContainerStatuses {
						contStatus = append(contStatus, s)
					}
				}
			}

			if err == nil {
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
			ixia.Status.State = "FAILED"
			ixia.Status.Reason = err.Error()
			// Ensure this is an end state, no need to requeue
			requeue = false
		} else {
			ixia.Status.State = "DEPLOYED"
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
			return errors.New(fmt.Sprintf("Dependency info could not be located for release version %s", release))
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
		return errors.New(fmt.Sprintf("Dependency info could not be located for release version %s", release))
	}

	return nil
}

func (r *IxiaTGReconciler) deleteIxiaPod(ctx context.Context, name string, ixia *networkv1alpha1.IxiaTG) error {
	found := &corev1.Pod{}
	if r.Get(ctx, types.NamespacedName{Name: name, Namespace: ixia.Namespace}, found) == nil {
		if err := r.Delete(ctx, found); err != nil {
			log.Errorf("Failed to delete ixia pod %v - %v", found, err)
			return err
		}
		log.Infof("Deleted ixia pod %v", found)
	}

	// Now delete the services
	service := &corev1.Service{}
	if r.Get(ctx, types.NamespacedName{Name: "service-" + name, Namespace: ixia.Namespace}, service) == nil {
		if err := r.Delete(ctx, service); err != nil {
			log.Errorf("Failed to delete ixia pod service %v - %v", service, err)
			return err
		}
		log.Infof("Deleted ixia pod service %v", service)
	}

	return nil
}

func (r *IxiaTGReconciler) deleteController(ctx context.Context, ixia *networkv1alpha1.IxiaTG) error {
	found := &corev1.Pod{}
	ctrlPodName := ixia.Name + "-controller"
	if r.Get(ctx, types.NamespacedName{Name: ctrlPodName, Namespace: ixia.Namespace}, found) == nil {
		if err := r.Delete(ctx, found); err != nil {
			log.Errorf("Failed to delete associated controller %v - %v", found, err)
			return err
		}
		log.Infof("Deleted controller %v", found)
	}

	// Now delete the config map
	cfgMapName := "controller-config"
	ctrlCfgMap := &corev1.ConfigMap{}
	if r.Get(ctx, types.NamespacedName{Name: cfgMapName, Namespace: ixia.Namespace}, ctrlCfgMap) == nil {
		if err := r.Delete(ctx, ctrlCfgMap); err != nil {
			log.Errorf("Failed to delete config map %v - %v", ctrlCfgMap, err)
			return err
		}
		log.Infof("Deleted config map %v", ctrlCfgMap)
	}

	// Now delete the services
	service := &corev1.Service{}
	for name, _ := range ixia.Spec.ApiEndPoint {
		if r.Get(ctx, types.NamespacedName{Name: "service-" + name + "-" + ixia.Name + "-controller", Namespace: ixia.Namespace}, service) == nil {
			if err := r.Delete(ctx, service); err != nil {
				log.Errorf("Failed to delete controller service %v - %v", service, err)
				return err
			}
			log.Infof("Deleted controller service %v", service)
		}
	}

	return nil
}

func (r *IxiaTGReconciler) deployController(ctx context.Context, podMap *map[string][]string, ixia *networkv1alpha1.IxiaTG, secret *corev1.Secret) error {
	var err error

	// First check if we have the component dependency data for the release
	depVersion := DEFAULT_VERSION
	if ixia.Spec.Release != "" {
		depVersion = ixia.Spec.Release
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

	// Deploy controller and services
	imagePullSecrets := getImgPullSctSecret(secret)
	containers, err := r.containersForController(ixia, depVersion)
	if err != nil {
		return err
	}

	locations := []location{}
	for podName, intfs := range *podMap {
		podSvc := "service-" + podName + "." + ixia.Namespace + ".svc.cluster.local"
		svcLoc := podSvc + ":5555+" + podSvc + ":50071"
		for _, intf := range intfs {
			locations = append(locations, location{Location: intf, EndPoint: svcLoc})
		}
	}
	mappings := controllerMap{LocationMap: locations}
	log.Infof("Prepared the location map object: %v", mappings)

	yamlObj, err := yaml.Marshal(&mappings)
	if err != nil {
		return err
	}

	cfgMapName := "controller-config"
	intfMap := make(map[string]string)
	intfMap["config.yaml"] = string(yamlObj)
	ctrlCfgMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgMapName,
			Namespace: ixia.Namespace,
		},
		Data: intfMap,
	}
	err = r.Create(ctx, ctrlCfgMap)
	if err != nil {
		log.Errorf("Failed to create config map controller-config in %v, err %v", ixia.Namespace, err)
		return err
	}
	log.Infof("Created the controller location mappings: %v", ctrlCfgMap)

	localObjRef := corev1.LocalObjectReference{Name: cfgMapName}
	cfgMapVolSrc := &corev1.ConfigMapVolumeSource{LocalObjectReference: localObjRef}
	volSrc := corev1.VolumeSource{ConfigMap: cfgMapVolSrc}
	volume := corev1.Volume{Name: "config", VolumeSource: volSrc}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ixia.Name + "-controller",
			Namespace: ixia.Namespace,
			Labels: map[string]string{
				"app": ixia.Name + "-controller",
			},
		},
		Spec: corev1.PodSpec{
			Containers:                    containers,
			ImagePullSecrets:              imagePullSecrets,
			TerminationGracePeriodSeconds: pointer.Int64(5),
			Volumes:                       []corev1.Volume{volume},
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

func (r *IxiaTGReconciler) podForIxia(ctx context.Context, podName string, intfList []string, ixia *networkv1alpha1.IxiaTG, secret *corev1.Secret) error {
	initContainers := []corev1.Container{}
	versionToDeploy := latestVersion
	if ixia.Spec.Release != "" && ixia.Spec.Release != DEFAULT_VERSION {
		versionToDeploy = ixia.Spec.Release
	}
	contPodMap := componentDep[versionToDeploy].Ixia.Containers
	args := []string{strconv.Itoa(len(intfList) + 1), "10"}
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
			Name:      podName,
			Namespace: ixia.Namespace,
			Labels: map[string]string{
				"app":  podName,
				"topo": ixia.Namespace,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers:                initContainers,
			Containers:                    r.containersForIxia(podName, intfList, ixia),
			ImagePullSecrets:              imagePullSecrets,
			TerminationGracePeriodSeconds: pointer.Int64(5),
		},
	}
	err := r.Create(ctx, pod)
	if err != nil {
		return err
	}

	// Now create corresponding services
	svcPorts := []corev1.ServicePort{}
	svcPorts = append(svcPorts, corev1.ServicePort{Name: "port-5555", Port: 5555, TargetPort: intstr.IntOrString{IntVal: 5555}})
	svcPorts = append(svcPorts, corev1.ServicePort{Name: "port-50071", Port: 50071, TargetPort: intstr.IntOrString{IntVal: 50071}})
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service-" + podName,
			Namespace: ixia.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": podName,
			},
			Ports: svcPorts,
			Type:  "LoadBalancer",
		},
	}
	err = r.Create(ctx, service)
	if err != nil {
		return err
	}

	return nil
}

func (r *IxiaTGReconciler) getControllerService(ixia *networkv1alpha1.IxiaTG) []corev1.Service {
	var services []corev1.Service

	// Ensure default ixia-c service is create from grpc/gnmi to communicate with controller
	for name, svc := range ixia.Spec.ApiEndPoint {
		contPort := corev1.ServicePort{Name: name, Port: svc.In, TargetPort: intstr.IntOrString{IntVal: svc.In}}
		services = append(services, corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-" + name + "-" + ixia.Name + "-controller",
				Namespace: ixia.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": ixia.Name + "-controller",
				},
				Ports: []corev1.ServicePort{contPort},
				Type:  "LoadBalancer",
			},
		})
	}

	return services
}

func updateControllerContainer(cont corev1.Container, pubRel componentRel, args []string, cmd []string) corev1.Container {
	cfgMapEnv := make(map[string]string)
	for ek, ev := range pubRel.Env {
		cfgMapEnv[ek] = ev.(string)
	}
	conEnv := getEnvData(nil, cfgMapEnv, nil)
	//if len(pubRel.Args) > 0 {
	//	args = pubRel.Args
	//}
	//if len(pubRel.Command) > 0 {
	//	cmd = pubRel.Command
	//}

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
		volMount := corev1.VolumeMount{Name: "config", ReadOnly: true, MountPath: "/home/keysight/ixia-c/controller/config"}
		log.Infof("Adding Pod: %s, Container: %s, Image: %s", CONTROLLER_NAME, name, image)
		container := updateControllerContainer(corev1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: "IfNotPresent",
			VolumeMounts:    []corev1.VolumeMount{volMount},
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
		command := []string{"python3", "-m", "grpc_server", "--app-mode", "athena", "--target-host", "localhost", "--target-port", "443", "--log-stdout", "--log-debug"}
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
		command := []string{"python3", "-m", "otg_gnmi", "--server-port", "50051", "--app-mode", "athena", "--target-host", "localhost", "--target-port", "443", "--insecure"}
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

	log.Infof("Done containersForController total containers %v!", len(containers))
	return containers, nil
}

func (r *IxiaTGReconciler) containersForIxia(podName string, intfList []string, ixia *networkv1alpha1.IxiaTG) []corev1.Container {
	//log.Infof("Inside containersForIxia: %v", ixia.Spec.Config)
	argIntfList := "virtual@af_packet," + intfList[0]

	conEnvs := map[string]string{
		"OPT_LISTEN_PORT":  "5555",
		"ARG_CORE_LIST":    "2 3 4",
		"ARG_IFACE_LIST":   argIntfList,
		"OPT_NO_HUGEPAGES": "Yes",
	}
	conSecurityCtx := getDefaultSecurityContext()
	var containers []corev1.Container
	config := &topopb.Config{}

	//if ixia.Spec.Config != "" {
	//	err := json.Unmarshal([]byte(ixia.Spec.Config), config)
	//	if err != nil {
	//		log.Infof("config unmarshalling failed: %v", err)
	//	}
	//}

	versionToDeploy := latestVersion
	if ixia.Spec.Release != "" && ixia.Spec.Release != DEFAULT_VERSION {
		versionToDeploy = ixia.Spec.Release
	}
	for k, v := range componentDep[versionToDeploy].Ixia.Containers {
		if strings.HasPrefix(v.Name, "init-") {
			continue
		}
		log.Infof("Deploying %s version %s for config version %s, ns %s (source %s)", k, v.Tag, versionToDeploy, ixia.Namespace, componentDep[versionToDeploy].Source)

		name := podName + "-" + k
		image := v.Path + ":" + v.Tag
		cfgMapEnv := make(map[string]string)
		log.Infof("Adding Pod: %s, Container: %s, Image: %s", podName, name, image)
		container := corev1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: conSecurityCtx,
		}
		if len(v.Args) > 0 {
			container.Args = v.Args
		}
		if len(v.Command) > 0 {
			container.Command = v.Command
		}
		if v.Name == "protocol-engine" {
			cfgMapEnv["INTF_LIST"] = strings.Join(intfList, ",")
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
