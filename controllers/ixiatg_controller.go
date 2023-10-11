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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	networkv1beta1 "github.com/open-traffic-generator/ixia-c-operator/api/v1beta1"

	log "github.com/sirupsen/logrus"

	"gopkg.in/yaml.v2"

	version "github.com/hashicorp/go-version"
)

const (
	CURRENT_NAMESPACE string = "ixiatg-op-system"
	SECRET_NAME       string = "ixia-pull-secret"

	LIC_ADDR_SECRET   string = "license-server"
	LIC_SERVER_SECRET string = "license-server-image"
	LIC_ENV_VAR       string = "LICENSE_SERVERS"

	SERVER_URL        string = "https://github.com/open-traffic-generator/ixia-c/releases/download/v"
	SERVER_LATEST_URL string = "https://github.com/open-traffic-generator/ixia-c/releases/latest/download"
	RELEASE_FILE      string = "/ixiatg-configmap.yaml"

	CONTROLLER_NAME string = "ixia-c"
	GRPC_NAME       string = "grpc"
	GNMI_NAME       string = "gnmi"
	LICENSE_NAME    string = "license-server"

	CONFIG_MAP_NAME      string = "ixiatg-release-config"
	CONFIG_MAP_NAMESPACE string = "ixiatg-op-system"
	DEFAULT_VERSION      string = "latest"
	DEFAULT_INTF         string = "eth1"

	DS_RESTAPI         string = "Rest API"
	DS_CONFIGMAP       string = "Config Map"
	CONTROLLER_SERVICE string = "ixia-c-service"
	GRPC_SERVICE       string = "grpc-service"
	GNMI_SERVICE       string = "gnmi-service"

	PORT_NAME_INFIX       string = "-port-"
	PORT_GROUP_NAME_INFIX string = "-port-group-"
	CTRL_POD_NAME_SUFFIX  string = "-controller"
	INIT_CONT_NAME_PREFIX string = "init-"

	CTRL_CFG_MAP_NAME   string = "controller-config"
	CTRL_MAP_VOL_NAME   string = "config"
	CTRL_MAP_FILE_NAME  string = "config.yaml"
	CTRL_MAP_MOUNT_PATH string = "/home/ixia-c/controller/config"

	SERVICE_NAME_SUFFIX string = ".svc.cluster.local"

	CTRL_HTTPS_PORT   int32 = 8443
	CTRL_GNMI_PORT    int32 = 50051
	CTRL_GRPC_PORT    int32 = 40051
	CTRL_LICENSE_PORT int32 = 7443
	PROTOCOL_ENG_PORT int32 = 50071
	TRAFFIC_ENG_PORT  int32 = 5555

	STATE_INITED   string = "INITIATED"
	STATE_DEPLOYED string = "DEPLOYED"
	STATE_FAILED   string = "FAILED"

	IMAGE_CONTROLLER     string = "controller"
	IMAGE_GNMI_SERVER    string = "gnmi-server"
	IMAGE_GRPC_SERVER    string = "grpc-server"
	IMAGE_LICENSE_SERVER string = "license-server"
	IMAGE_LICENSE_SECRET string = "license-server-default"
	IMAGE_TRAFFIC_ENG    string = "traffic-engine"
	IMAGE_PROTOCOL_ENG   string = "protocol-engine"

	TERMINATION_TIMEOUT_SEC int64 = 5

	HTTP_TIMEOUT_SEC time.Duration = 5

	GNMI_NEW_BASE_VERSION string = "1.7.9"
	IXIA_C_OTG_VERSION    string = "0.0.1-2727"
	IXIA_C_GRPC_VERSION   string = "0.0.1-3114"

	LIVENESS_DELAY   int32 = 1
	LIVENESS_PERIOD  int32 = 10
	LIVENESS_FAILURE int32 = 6

	MIN_MEM_CONTROLLER string = "25Mi"
	MIN_MEM_GNMI       string = "15Mi"
	MIN_CPU_PROTOCOL   string = "200m"
	MIN_CPU_TRAFFIC    string = "200m"
	MIN_CPU_CONTROLLER string = "10m"
	MIN_CPU_GNMI       string = "10m"
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
	Args            []string `json:"args"`
	Command         []string `json:"command"`
	ContainerName   string
	DefArgs         []string
	DefCmd          []string
	DefEnv          map[string]string
	Env             map[string]interface{} `json:"env"`
	LiveNessEnable  *bool                  `json:"liveness-enable,omitempty"`
	LiveNessDelay   int32                  `json:"liveness-initial-delay,omitempty"`
	LiveNessPeriod  int32                  `json:"liveness-period,omitempty"`
	LiveNessFailure int32                  `json:"liveness-failure,omitempty"`
	MinResource     map[string]string      `json:"min-resource"`
	Name            string                 `json:"name"`
	Path            string                 `json:"path"`
	Port            int32
	Tag             string `json:"tag"`
	VolMntName      string
	VolMntPath      string
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

	ixia := &networkv1beta1.IxiaTG{}
	err := r.Get(ctx, req.NamespacedName, ixia)

	log.Infof("Reconcile: %v (Desired State: %v), Namespace: %s", ixia.Name, ixia.Spec.DesiredState, ixia.Namespace)

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
			// Delete secrets, if copied
			if err = r.DeleteSecrets(ctx, ixia.Namespace); err != nil {
				return ctrl.Result{}, err
			}
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
	otgCtrlName := ixia.Name + CTRL_POD_NAME_SUFFIX
	log.Infof("Desired State: %v, Current State: %v", ixia.Spec.DesiredState, ixia.Status.State)
	if ixia.Spec.DesiredState == ixia.Status.State {
		return ctrl.Result{}, nil
	} else if ixia.Spec.DesiredState == STATE_INITED {
		otgCtrl, err := r.deployController(ctx, nil, ixia, true)
		if err == nil {
			log.Infof("Controller version for OTG %v", otgCtrl)
			// For OTG model check for multiple OTGs; otherwise for multiple Ixia nodes check for all versions match
			crdList := &networkv1beta1.IxiaTGList{}
			opts := []client.ListOption{
				client.InNamespace(ixia.Namespace),
			}
			err = r.List(ctx, crdList, opts...)
			if err != nil {
				log.Errorf("Failed to get list of IxiaTG nodes - %v", err)
			} else if otgCtrl && len(crdList.Items) > 1 {
				err = errors.New(fmt.Sprintf("Unsupported configuration; multiple (%d) OTG nodes specified", len(crdList.Items)))
			} else if !otgCtrl && len(ixia.Spec.Interfaces) > 1 {
				err = errors.New(fmt.Sprintf("Multiple interfaces (%d) specified for node %s", len(ixia.Spec.Interfaces), ixia.Name))
			} else if !otgCtrl && ixia.Spec.Interfaces[0].Name != DEFAULT_INTF {
				err = errors.New(fmt.Sprintf("Unsupported interface %s for Controller version; interface must be eth1", ixia.Spec.Interfaces[0].Name))
			} else if !otgCtrl && ixia.Name == CONTROLLER_NAME {
				err = errors.New(fmt.Sprintf("Node name %s is reserved for Controller pod, use some other name", CONTROLLER_NAME))
			} else {
				if !otgCtrl && len(crdList.Items) > 1 {
					specVer := crdList.Items[0].Spec.Release
					for _, node := range crdList.Items {
						if node.Spec.Release != specVer {
							err = errors.New(fmt.Sprintf("IxiaTG node versions are not consistent; found %s for one and %s for another", node.Spec.Release, specVer))
							break
						}
					}
				}

				if err == nil {
					genPodNames := []networkv1beta1.IxiaTGIntfStatus{}
					for _, intf := range ixia.Spec.Interfaces {
						deployIntf := DEFAULT_INTF
						podName := ixia.Name
						if otgCtrl {
							deployIntf = intf.Name
							podName = ixia.Name + PORT_NAME_INFIX + intf.Name
						}
						if intf.Group != "" {
							if !otgCtrl {
								err = errors.New(fmt.Sprintf("Group, in config, is not supported for version older than %s", IXIA_C_OTG_VERSION))
								break
							}
							podName = ixia.Name + PORT_GROUP_NAME_INFIX + intf.Group
						}
						genPodNames = append(genPodNames,
							networkv1beta1.IxiaTGIntfStatus{PodName: podName, Name: intf.Name, Intf: deployIntf})
					}

					if err == nil {
						svcList := []string{}
						podName := ixia.Name
						if otgCtrl {
							podName = otgCtrlName
							for name, _ := range ixia.Spec.ApiEndPoint {
								svcList = append(svcList, "service-"+name+"-"+otgCtrlName)
							}
						} else {
							svcList = append(svcList, "service-"+podName)
						}
						genSvcEP := networkv1beta1.IxiaTGSvcEP{PodName: podName, ServiceName: svcList}
						log.Infof("Node update with interfaces: %v", genPodNames)
						ixia.Status.Interfaces = genPodNames
						ixia.Status.State = ixia.Spec.DesiredState
						ixia.Status.ApiEndPoint = genSvcEP
					}
				}
			}
		}

		if err != nil {
			log.Error(err)
			ixia.Status.Reason = err.Error()
			ixia.Status.State = STATE_FAILED
		}

		err = r.Status().Update(ctx, ixia)
		if err != nil {
			log.Errorf("Failed to update ixia status - %v", err)
			return ctrl.Result{RequeueAfter: time.Second}, err
		}

		return ctrl.Result{}, nil
	} else if ixia.Spec.DesiredState != STATE_DEPLOYED {
		err = errors.New(fmt.Sprintf("Unknown desired state found %s", ixia.Spec.DesiredState))
		log.Error(err)
		ixia.Status.Reason = err.Error()
		ixia.Status.State = STATE_FAILED

		err = r.Status().Update(ctx, ixia)
		if err != nil {
			log.Errorf("Failed to update ixia status - %v", err)
			return ctrl.Result{RequeueAfter: time.Second}, err
		}

		return ctrl.Result{}, nil
	}

	// Check if we need to create resources or not
	err = r.ReconcileSecrets(ctx, req, ixia)

	requeue := false
	found := &corev1.Pod{}
	otgCtrl, err := r.deployController(ctx, nil, ixia, true)
	if err == nil {
		if !otgCtrl {
			otgCtrlName = ixia.Name
		}
		err = r.Get(ctx, types.NamespacedName{Name: otgCtrlName, Namespace: ixia.Namespace}, found)
	}
	if err != nil && errapi.IsNotFound(err) {
		// need to deploy, but first deploy controller if not present
		podMap := make(map[string][]string)
		for _, intf := range ixia.Status.Interfaces {
			if _, ok := podMap[intf.PodName]; ok {
				podMap[intf.PodName] = append(podMap[intf.PodName], intf.Intf)
			} else {
				podMap[intf.PodName] = []string{intf.Intf}
			}
		}
		log.Infof("Deployment interface map created: %v", podMap)
		if _, err = r.deployController(ctx, &podMap, ixia, false); err == nil {
			log.Infof("Successfully deployed controller pod")
			for name, intfs := range podMap {
				log.Infof("Creating pod %v", name)
				if err = r.podForIxia(ctx, name, intfs, ixia); err != nil {
					log.Infof("Pod %v create failed!", name)
					break
				}
				log.Infof("Pod %v created!", name)
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
		if found.Status.Phase == corev1.PodFailed {
			err = errors.New(fmt.Sprintf("Pod %s failed - %s", found.Name, found.Status.Reason))
		} else if err == nil {
			var contStatus []corev1.ContainerStatus
			if found.Status.Phase != corev1.PodRunning {
				requeue = true
				for _, s := range found.Status.ContainerStatuses {
					contStatus = append(contStatus, s)
				}
			}
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
						msg := c.State.Waiting.Message
						if strings.Contains(msg, "repository does not exist") || strings.Contains(msg, "access to the resource is denied") {
							err = errors.New(fmt.Sprintf("Container %s failed - %s", c.Name, c.State.Waiting.Message))
							break
						}
					}
				}
			}
		}
	}

	if !requeue || err != nil {
		if err != nil {
			ixia.Status.State = STATE_FAILED
			ixia.Status.Reason = err.Error()
			// Ensure this is an end state, no need to requeue
			requeue = false
		} else {
			ixia.Status.State = STATE_DEPLOYED
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

func (r *IxiaTGReconciler) getRelInfo(ctx context.Context, release string, namespace string) error {
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
		Timeout: HTTP_TIMEOUT_SEC * time.Second,
	}
	resp, err := client.Get(url)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			yamlData, err := ioutil.ReadAll(resp.Body)
			if err == nil {
				var yamlCfg ixiaConfigMap
				err = yaml.Unmarshal([]byte(yamlData), &yamlCfg)
				if err == nil {
					data = []byte(yamlCfg.Data.Versions)
				}
			}
			if err != nil {
				log.Errorf("Failed to parse downloaded release config file - %v", err)
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
			return errors.New(fmt.Sprintf("Dependency info for version %s could not be located; ensure configmap with that version is loaded", release))
		}

		return nil
	}

	return r.loadRelInfo(ctx, release, &data, false, source, namespace)
}

func (r *IxiaTGReconciler) loadRelInfo(ctx context.Context, release string, relData *[]byte, list bool, source string, ns string) error {
	var rel pubRel
	var relList pubReleases
	var err error
	initContSeq := 0

	if list {
		err = json.Unmarshal(*relData, &relList)
	} else {
		err = json.Unmarshal(*relData, &rel)
		if err == nil {
			relList = pubReleases{}
			relList.Releases = append(relList.Releases, rel)
		}
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
			var compRef componentRel
			ctrlComponent := false
			contKeyName := image.Name
			switch image.Name {
			case IMAGE_CONTROLLER:
				fallthrough
			case IMAGE_GNMI_SERVER:
				fallthrough
			case IMAGE_LICENSE_SERVER:
				fallthrough
			case IMAGE_GRPC_SERVER:
				topoEntry.Controller.Containers[contKeyName] = image
				compRef = topoEntry.Controller.Containers[contKeyName]
				ctrlComponent = true
			case IMAGE_TRAFFIC_ENG:
				fallthrough
			case IMAGE_PROTOCOL_ENG:
				topoEntry.Ixia.Containers[contKeyName] = image
				compRef = topoEntry.Ixia.Containers[contKeyName]
			default:
				if strings.HasPrefix(image.Name, INIT_CONT_NAME_PREFIX) {
					initContSeq = initContSeq + 1
					contKeyName = fmt.Sprintf("init%d", initContSeq)
					topoEntry.Ixia.Containers[contKeyName] = image
					compRef = topoEntry.Ixia.Containers[contKeyName]
				} else {
					log.Errorf("Error unknown image name: %s (ignoring)", image.Name)
					continue
				}
			}

			// Now update defaults
			switch contKeyName {
			case IMAGE_CONTROLLER:
				compRef.ContainerName = CONTROLLER_NAME
				compRef.DefArgs = []string{"--accept-eula", "--debug"}
				compRef.VolMntName = CTRL_MAP_VOL_NAME
				compRef.VolMntPath = CTRL_MAP_MOUNT_PATH
			case IMAGE_GNMI_SERVER:
				compRef.ContainerName = GNMI_NAME
				compRef.DefCmd = []string{"python3", "-m", "otg_gnmi", "--server-port", strconv.Itoa(int(CTRL_GNMI_PORT)), "--app-mode", "athena", "--target-host", "localhost", "--target-port", strconv.Itoa(int(CTRL_HTTPS_PORT)), "--insecure"}
				compRef.Port = CTRL_GNMI_PORT
			case IMAGE_GRPC_SERVER:
				compRef.ContainerName = GRPC_NAME
				compRef.DefCmd = []string{"python3", "-m", "grpc_server", "--app-mode", "athena", "--target-host", "localhost", "--target-port", strconv.Itoa(int(CTRL_HTTPS_PORT)), "--log-stdout", "--log-debug"}
				compRef.Port = CTRL_GRPC_PORT
			case IMAGE_LICENSE_SERVER:
				compRef.ContainerName = LICENSE_NAME
				compRef.DefArgs = []string{"--accept-eula", "--debug"}
				compRef.Port = CTRL_LICENSE_PORT
			case IMAGE_TRAFFIC_ENG:
				compRef.ContainerName = IMAGE_TRAFFIC_ENG
				compRef.DefEnv = map[string]string{
					"OPT_LISTEN_PORT":  strconv.Itoa(int(TRAFFIC_ENG_PORT)),
					"ARG_CORE_LIST":    "2 3 4",
					"ARG_IFACE_LIST":   "virtual@af_packet,eth1",
					"OPT_NO_HUGEPAGES": "Yes",
				}
			case IMAGE_PROTOCOL_ENG:
				compRef.ContainerName = IMAGE_PROTOCOL_ENG
			default:
				compRef.ContainerName = compRef.Name
			}

			// For all components update health check parameters
			if compRef.LiveNessDelay == 0 {
				compRef.LiveNessDelay = LIVENESS_DELAY
			}
			if compRef.LiveNessPeriod == 0 {
				compRef.LiveNessPeriod = LIVENESS_PERIOD
			}
			if compRef.LiveNessFailure == 0 {
				compRef.LiveNessFailure = LIVENESS_FAILURE
			}

			if ctrlComponent {
				topoEntry.Controller.Containers[contKeyName] = compRef
			} else {
				topoEntry.Ixia.Containers[contKeyName] = compRef
			}
		}

		// From ixia-c release version IXIA_C_GRPC_VERSION, the gRPC container functionality has been merged
		// into ixia-c container; so remove any gRPC release mapping and also update ixia-c default command.
		if ctrl, ok := topoEntry.Controller.Containers[IMAGE_CONTROLLER]; ok {
			noGRPC, err := versionLaterOrEqual(IXIA_C_GRPC_VERSION, ctrl.Tag)
			if err != nil {
				return err
			}
			if noGRPC {
				// Remove any gRPC component
				delete(topoEntry.Controller.Containers, IMAGE_GRPC_SERVER)
				ctrl.DefArgs = []string{"--accept-eula", "--debug", "--grpc-port", "40051"}
			}
		}

		// License server may not be part of configmap always, we always add a default entry if corresponding secret is found
		delete(topoEntry.Controller.Containers, IMAGE_LICENSE_SECRET)
		if secret, err := r.GetSecret(ctx, LIC_SERVER_SECRET, ns); err != nil {
			return fmt.Errorf("Failed to determine secret %s - %v", LIC_SERVER_SECRET, err)
		} else if secret != nil {
			if licImage, ok := secret.Data["image_path"]; ok {
				compRef := componentRel{Name: IMAGE_LICENSE_SERVER, Path: string(licImage)}
				compRef.ContainerName = LICENSE_NAME
				compRef.DefArgs = []string{"--accept-eula", "--debug"}
				compRef.Port = CTRL_LICENSE_PORT
				topoEntry.Controller.Containers[IMAGE_LICENSE_SECRET] = compRef
			}
		}

		componentDep[relEntry.Release] = topoEntry
		if release == DEFAULT_VERSION {
			latestVersion = relEntry.Release
			release = latestVersion
		}
		log.Infof("Found version info for %s through %s", relEntry.Release, source)
		log.Infof("Mapped controller components:")
		for key, val := range topoEntry.Controller.Containers {
			log.Infof("Component Added (key %s): %+v", key, val)
		}
		log.Infof("Mapped ixiatg node components:")
		for key, val := range topoEntry.Ixia.Containers {
			log.Infof("Component Added (key %s): %+v", key, val)
		}
	}

	if _, ok := componentDep[release]; !ok {
		log.Errorf("Release %s related dependency could not be located", release)
		return errors.New(fmt.Sprintf("Dependency info for version %s could not be located; ensure configmap with that version is loaded", release))
	}

	return nil
}

func (r *IxiaTGReconciler) deleteIxiaPod(ctx context.Context, name string, ixia *networkv1beta1.IxiaTG) error {
	found := &corev1.Pod{}
	if r.Get(ctx, types.NamespacedName{Name: name, Namespace: ixia.Namespace}, found) == nil {
		if err := r.Delete(ctx, found, client.GracePeriodSeconds(5)); err != nil {
			log.Errorf("Failed to delete ixia pod %v - %v", found, err)
			return err
		}
		log.Infof("Deleted ixia pod %v", found)
	}

	// Now delete the services
	service := &corev1.Service{}
	if r.Get(ctx, types.NamespacedName{Name: "service-" + name, Namespace: ixia.Namespace}, service) == nil {
		if err := r.Delete(ctx, service, client.GracePeriodSeconds(0)); err != nil {
			log.Errorf("Failed to delete ixia pod service %v - %v", service, err)
			return err
		}
		log.Infof("Deleted ixia pod service %v", service)
	}

	return nil
}

func (r *IxiaTGReconciler) deleteController(ctx context.Context, ixia *networkv1beta1.IxiaTG) error {
	found := &corev1.Pod{}
	ctrlPodName := ixia.Name + "-controller"
	if r.Get(ctx, types.NamespacedName{Name: ctrlPodName, Namespace: ixia.Namespace}, found) == nil {
		if err := r.Delete(ctx, found, client.GracePeriodSeconds(5)); err != nil {
			log.Errorf("Failed to delete associated controller %v - %v", found, err)
			return err
		}
		log.Infof("Deleted controller %v", found)
	}

	// Now delete the config map
	ctrlCfgMap := &corev1.ConfigMap{}
	if r.Get(ctx, types.NamespacedName{Name: CTRL_CFG_MAP_NAME, Namespace: ixia.Namespace}, ctrlCfgMap) == nil {
		if err := r.Delete(ctx, ctrlCfgMap, client.GracePeriodSeconds(0)); err != nil {
			log.Errorf("Failed to delete config map %v - %v", ctrlCfgMap, err)
			return err
		}
		log.Infof("Deleted config map %v", ctrlCfgMap)
	}

	// Now delete the services
	service := &corev1.Service{}
	for name, _ := range ixia.Spec.ApiEndPoint {
		if r.Get(ctx, types.NamespacedName{Name: "service-" + name + "-" + ctrlPodName, Namespace: ixia.Namespace}, service) == nil {
			if err := r.Delete(ctx, service, client.GracePeriodSeconds(0)); err != nil {
				log.Errorf("Failed to delete controller service %v - %v", service, err)
				return err
			}
			log.Infof("Deleted controller service %v", service)
		}
	}

	return nil
}

func (r *IxiaTGReconciler) deployController(ctx context.Context, podMap *map[string][]string, ixia *networkv1beta1.IxiaTG, checkOtgOnly bool) (bool, error) {
	var err error
	isOtgCtrl := true
	podList := &corev1.PodList{}
	opts := []client.ListOption{
		client.InNamespace(ixia.Namespace),
	}

	// First check if we have the component dependency data for the release
	depVersion := DEFAULT_VERSION
	if ixia.Spec.Release != "" {
		depVersion = ixia.Spec.Release
	} else {
		log.Infof("No ixiatg version specified, using default version %s", depVersion)
	}

	if _, ok := componentDep[depVersion]; !ok || depVersion == DEFAULT_VERSION || componentDep[depVersion].Source == DS_CONFIGMAP {
		if err = r.getRelInfo(ctx, depVersion, ixia.Namespace); err != nil {
			log.Errorf("Failed to get release information for %s", depVersion)
			return isOtgCtrl, err
		}
	}
	if depVersion == DEFAULT_VERSION {
		if latestVersion == "" {
			log.Errorf("Failed to get release information for %s", depVersion)
			return isOtgCtrl, errors.New(fmt.Sprintf("Failed to get release information for version %s", DEFAULT_VERSION))
		} else {
			depVersion = latestVersion
		}
	}

	// Determine if Controller supports new OTG model
	found := false
	for _, comp := range componentDep[depVersion].Controller.Containers {
		if comp.ContainerName == CONTROLLER_NAME {
			found = true
			isOtgCtrl, err = versionLaterOrEqual(IXIA_C_OTG_VERSION, comp.Tag)
			break
		}
	}
	if !found || err != nil {
		return isOtgCtrl, errors.New(fmt.Sprintf("Failed to locate Controller entry for version %s in configmap", depVersion))
	}
	if checkOtgOnly {
		return isOtgCtrl, nil
	} else if !isOtgCtrl {
		// Skip if already deployed for old KNE configs
		if err = r.List(ctx, podList, opts...); err != nil {
			log.Errorf("Failed to list current pods %v", err)
			return isOtgCtrl, err
		}
		for _, p := range podList.Items {
			if p.ObjectMeta.Name == CONTROLLER_NAME {
				log.Infof("Controller already deployed for %s", ixia.Name)
				return isOtgCtrl, nil
			}
		}
	}

	// Deploy controller and services
	imagePullSecrets := []corev1.LocalObjectReference{{Name: string(SECRET_NAME)}}
	containers, err := r.containersForController(ctx, ixia, depVersion, isOtgCtrl)
	if err != nil {
		return isOtgCtrl, err
	}

	locations := []location{}
	svcSuffix := ixia.Namespace + SERVICE_NAME_SUFFIX
	pePort := ":" + strconv.Itoa(int(PROTOCOL_ENG_PORT))
	tePort := ":" + strconv.Itoa(int(TRAFFIC_ENG_PORT))
	for podName, intfs := range *podMap {
		podSvc := "service-" + podName + "." + svcSuffix
		for index, intf := range intfs {
			svcLoc := podSvc + tePort + "+" + podSvc + pePort
			if len(intfs) > 1 {
				svcLoc = podSvc + tePort + ";" + strconv.Itoa(index+1) + "+" + podSvc + pePort
			}
			locations = append(locations, location{Location: intf, EndPoint: svcLoc})
		}
	}
	mappings := controllerMap{LocationMap: locations}
	log.Infof("Prepared the location map object: %v", mappings)

	yamlObj, err := yaml.Marshal(&mappings)
	if err != nil {
		return isOtgCtrl, err
	}

	intfMap := make(map[string]string)
	intfMap[CTRL_MAP_FILE_NAME] = string(yamlObj)
	ctrlCfgMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CTRL_CFG_MAP_NAME,
			Namespace: ixia.Namespace,
		},
		Data: intfMap,
	}
	err = r.Create(ctx, ctrlCfgMap)
	if err != nil {
		log.Errorf("Failed to create config map controller-config in %v, err %v", ixia.Namespace, err)
		return isOtgCtrl, err
	}
	log.Infof("Created the controller location mappings: %v", ctrlCfgMap)

	localObjRef := corev1.LocalObjectReference{Name: CTRL_CFG_MAP_NAME}
	cfgMapVolSrc := &corev1.ConfigMapVolumeSource{LocalObjectReference: localObjRef}
	volSrc := corev1.VolumeSource{ConfigMap: cfgMapVolSrc}
	volume := corev1.Volume{Name: CTRL_MAP_VOL_NAME, VolumeSource: volSrc}

	otgCtrlName := ixia.Name + CTRL_POD_NAME_SUFFIX
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      otgCtrlName,
			Namespace: ixia.Namespace,
			Labels: map[string]string{
				"app": otgCtrlName,
			},
		},
		Spec: corev1.PodSpec{
			Containers:                    containers,
			ImagePullSecrets:              imagePullSecrets,
			TerminationGracePeriodSeconds: pointer.Int64(TERMINATION_TIMEOUT_SEC),
		},
	}
	if isOtgCtrl {
		pod.Spec.Volumes = []corev1.Volume{volume}
	} else {
		pod.ObjectMeta.Name = CONTROLLER_NAME
	}
	log.Infof("Creating controller pod %v", pod)
	err = r.Create(ctx, pod)
	if err != nil {
		log.Errorf("Failed to create pod %v in %v, err %v", pod.Name, pod.Namespace, err)
		return isOtgCtrl, err
	}

	// Now create and map services
	services := r.getControllerService(ixia, isOtgCtrl)
	for _, s := range services {
		err = r.Create(ctx, &s)
		if err != nil {
			log.Errorf("Failed to create service %v in %v, err %v", s, ixia.Namespace, err)
			return isOtgCtrl, err
		}
	}

	return isOtgCtrl, nil
}

func (r *IxiaTGReconciler) podForIxia(ctx context.Context, podName string, intfList []string, ixia *networkv1beta1.IxiaTG) error {
	initContainers := []corev1.Container{}
	versionToDeploy := latestVersion
	if ixia.Spec.Release != "" && ixia.Spec.Release != DEFAULT_VERSION {
		versionToDeploy = ixia.Spec.Release
	}
	contPodMap := componentDep[versionToDeploy].Ixia.Containers
	args := []string{strconv.Itoa(len(intfList) + 1), "10"}
	initImage := "networkop/init-wait:latest"
	initContainerMsg := "Added default init container"
	if ixia.Spec.InitContainer.Image == "" {
		for _, cont := range contPodMap {
			if strings.HasPrefix(cont.Name, INIT_CONT_NAME_PREFIX) {
				initContainerMsg = "Added custom init container from configmap"
				initCont := corev1.Container{
					Name:            cont.ContainerName,
					Image:           cont.Path + ":" + cont.Tag,
					ImagePullPolicy: "IfNotPresent",
				}
				updateControllerContainer(&initCont, cont, false)
				// Since the args are dynamic based on topology deployment, we verify if args
				// are applied based on configmap spec; otherwise apply default args
				if len(initCont.Args) == 0 {
					initCont.Args = args
				}
				log.Infof("Adding init container image: %+v", initCont)
				initContainers = append(initContainers, initCont)
			}
		}
	} else {
		initContainerMsg = "Added custom init container from config spec"
		if ixia.Spec.InitContainer.Sleep != 0 {
			args[1] = fmt.Sprintf("%d", ixia.Spec.InitContainer.Sleep)
		}
		initImage = ixia.Spec.InitContainer.Image
	}
	if len(initContainers) == 0 {
		defaultInitCont := corev1.Container{
			Name:            "init-container",
			Image:           initImage,
			Args:            args,
			ImagePullPolicy: "IfNotPresent",
		}
		initContainers = append(initContainers, defaultInitCont)
	}
	log.Info(initContainerMsg)
	imagePullSecrets := []corev1.LocalObjectReference{{Name: string(SECRET_NAME)}}
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
			TerminationGracePeriodSeconds: pointer.Int64(TERMINATION_TIMEOUT_SEC),
		},
	}
	err := r.Create(ctx, pod)
	if err != nil {
		return err
	}

	// Now create corresponding services
	svcPorts := []corev1.ServicePort{}
	portName := "port-" + strconv.Itoa(int(TRAFFIC_ENG_PORT))
	svcPorts = append(svcPorts, corev1.ServicePort{Name: portName, Port: TRAFFIC_ENG_PORT, TargetPort: intstr.IntOrString{IntVal: TRAFFIC_ENG_PORT}})
	portName = "port-" + strconv.Itoa(int(PROTOCOL_ENG_PORT))
	svcPorts = append(svcPorts, corev1.ServicePort{Name: portName, Port: PROTOCOL_ENG_PORT, TargetPort: intstr.IntOrString{IntVal: PROTOCOL_ENG_PORT}})
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

func (r *IxiaTGReconciler) getControllerService(ixia *networkv1beta1.IxiaTG, isOtgCtrl bool) []corev1.Service {
	var services []corev1.Service

	// Ensure default ixia-c service is create from grpc/gnmi to communicate with controller
	ctrlPodName := ixia.Name + CTRL_POD_NAME_SUFFIX
	if isOtgCtrl {
		for name, svc := range ixia.Spec.ApiEndPoint {
			contPort := corev1.ServicePort{Name: name, Port: svc.In, TargetPort: intstr.IntOrString{IntVal: svc.In}}
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-" + name + "-" + ctrlPodName,
					Namespace: ixia.Namespace,
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app": ctrlPodName,
					},
					Ports: []corev1.ServicePort{contPort},
					Type:  "LoadBalancer",
				},
			}
			services = append(services, service)
		}
	} else {
		// Default HTTPS service
		contPort := corev1.ServicePort{Name: CONTROLLER_NAME, Port: CTRL_HTTPS_PORT, TargetPort: intstr.IntOrString{IntVal: CTRL_HTTPS_PORT}}
		services = append(services, corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      CONTROLLER_SERVICE,
				Namespace: ixia.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": ctrlPodName,
				},
				Ports: []corev1.ServicePort{contPort},
				Type:  "LoadBalancer",
			},
		})
		// Default gRPC service
		contPort = corev1.ServicePort{Name: GRPC_NAME, Port: CTRL_GRPC_PORT, TargetPort: intstr.IntOrString{IntVal: CTRL_GRPC_PORT}}
		services = append(services, corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GRPC_SERVICE,
				Namespace: ixia.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": ctrlPodName,
				},
				Ports: []corev1.ServicePort{contPort},
				Type:  "LoadBalancer",
			},
		})
		// Default gNMI service
		contPort = corev1.ServicePort{Name: GNMI_NAME, Port: CTRL_GNMI_PORT, TargetPort: intstr.IntOrString{IntVal: CTRL_GNMI_PORT}}
		services = append(services, corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      GNMI_SERVICE,
				Namespace: ixia.Namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": ctrlPodName,
				},
				Ports: []corev1.ServicePort{contPort},
				Type:  "LoadBalancer",
			},
		})
	}

	return services
}

func updateControllerContainer(cont *corev1.Container, pubRel componentRel, newGNMI bool) {
	conEnvs := []corev1.EnvVar{}
	cfgMapEnv := make(map[string]string)
	for ek, ev := range pubRel.DefEnv {
		cfgMapEnv[ek] = ev
	}
	for ek, ev := range pubRel.Env {
		cfgMapEnv[ek] = ev.(string)
	}

	for key, value := range cfgMapEnv {
		conEnvs = append(conEnvs, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	if len(pubRel.Args) > 0 {
		cont.Args = pubRel.Args
	} else if newGNMI {
		cont.Args = []string{"-http-server", "https://localhost:8443", "--debug"}
	} else if len(pubRel.DefArgs) > 0 {
		cont.Args = pubRel.DefArgs
	}
	if len(pubRel.Command) > 0 {
		cont.Command = pubRel.Command
	} else if newGNMI {
		cont.Command = []string{}
	} else if len(pubRel.DefCmd) > 0 {
		cont.Command = pubRel.DefCmd
	}
	if len(conEnvs) > 0 {
		cont.Env = conEnvs
	}
}

func versionLaterOrEqual(baseVer string, chkVer string) (bool, error) {
	base, err := version.NewVersion(baseVer)
	if err != nil {
		return false, errors.New(fmt.Sprintf("Failed to verify gNMI version (%s) - %v", baseVer, err))
	}

	chk, err := version.NewVersion(chkVer)
	if err != nil {
		return false, errors.New(fmt.Sprintf("Failed to verify gNMI version (%s) - %v", chkVer, err))
	} else if chk.GreaterThanOrEqual(base) {
		return true, nil
	}
	return false, nil
}

func (r *IxiaTGReconciler) containersForController(ctx context.Context, ixia *networkv1beta1.IxiaTG, release string, otg bool) ([]corev1.Container, error) {
	log.Infof("Get containers for Controller (release %s)", release)
	var containers []corev1.Container
	var newGNMI bool
	var err error
	var pbHdlr corev1.ProbeHandler
	lic_found := false
	lic_container := corev1.Container{}

	if _, ok := componentDep[release].Controller.Containers[IMAGE_CONTROLLER]; !ok {
		return nil, fmt.Errorf("Failed to find controller entry in configmap for release %s", release)
	}
	if _, ok := componentDep[release].Controller.Containers[IMAGE_GNMI_SERVER]; !ok {
		return nil, fmt.Errorf("Failed to find gNMI entry in configmap for release %s", release)
	}
	if ctrl, ok := componentDep[release].Controller.Containers[IMAGE_CONTROLLER]; ok {
		noGRPC, err := versionLaterOrEqual(IXIA_C_GRPC_VERSION, ctrl.Tag)
		if err != nil {
			log.Error(err)
		}
		if !noGRPC {
			if _, ok := componentDep[release].Controller.Containers[IMAGE_GRPC_SERVER]; !ok {
				return nil, fmt.Errorf("Failed to find gRPC entry in configmap for release %s", release)
			}
		}
	}
	for key, comp := range componentDep[release].Controller.Containers {
		if key == IMAGE_LICENSE_SERVER && len(lic_container.Name) > 0 {
			// Already found from secrets
			continue
		}
		name := comp.ContainerName
		image := comp.Path
		if comp.Tag != "" {
			image += ":" + comp.Tag
		}
		log.Infof("Deploying %s version %s for config version %s, ns %s (source %s)",
			name, comp.Tag, release, ixia.Namespace, componentDep[release].Source)
		container := corev1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: "IfNotPresent",
		}
		if comp.VolMntName != "" && otg {
			volMount := corev1.VolumeMount{Name: comp.VolMntName, ReadOnly: true, MountPath: comp.VolMntPath}
			container.VolumeMounts = []corev1.VolumeMount{volMount}
		}
		if comp.Port != 0 {
			port := corev1.ContainerPort{Name: comp.ContainerName, ContainerPort: comp.Port, Protocol: "TCP"}
			container.Ports = []corev1.ContainerPort{port}
		}
		newGNMI = false
		err = nil
		resRequest := corev1.ResourceList{}
		if r, ok := comp.MinResource["cpu"]; ok {
			resRequest["cpu"] = resource.MustParse(r)
		}
		if r, ok := comp.MinResource["memory"]; ok {
			resRequest["memory"] = resource.MustParse(r)
		}
		if name == GNMI_NAME {
			tcpSock := corev1.TCPSocketAction{Port: intstr.IntOrString{IntVal: CTRL_GNMI_PORT}}
			pbHdlr = corev1.ProbeHandler{TCPSocket: &tcpSock}
			newGNMI, err = versionLaterOrEqual(GNMI_NEW_BASE_VERSION, comp.Tag)
			if err != nil {
				log.Error(err)
			}
			if _, ok := resRequest["cpu"]; !ok {
				resRequest["cpu"] = resource.MustParse(MIN_CPU_GNMI)
			}
			if _, ok := resRequest["memory"]; !ok {
				resRequest["memory"] = resource.MustParse(MIN_MEM_GNMI)
			}
		} else if name == CONTROLLER_NAME {
			tcpSock := corev1.TCPSocketAction{Port: intstr.IntOrString{IntVal: CTRL_GRPC_PORT}}
			pbHdlr = corev1.ProbeHandler{TCPSocket: &tcpSock}
			if _, ok := resRequest["cpu"]; !ok {
				resRequest["cpu"] = resource.MustParse(MIN_CPU_CONTROLLER)
			}
			if _, ok := resRequest["memory"]; !ok {
				resRequest["memory"] = resource.MustParse(MIN_MEM_CONTROLLER)
			}
		}
		container.Resources.Requests = resRequest

		if comp.LiveNessEnable == nil || *comp.LiveNessEnable {
			probe := corev1.Probe{
				ProbeHandler:                  pbHdlr,
				InitialDelaySeconds:           comp.LiveNessDelay,
				PeriodSeconds:                 comp.LiveNessPeriod,
				FailureThreshold:              comp.LiveNessFailure,
				TerminationGracePeriodSeconds: pointer.Int64(1),
			}
			container.LivenessProbe = &probe
		}

		updateControllerContainer(&container, comp, newGNMI)
		// License server related handling
		if name == CONTROLLER_NAME {
			// First check is corresponding secret is present
			licAddr := ""
			if secret, err := r.GetSecret(ctx, LIC_ADDR_SECRET, ixia.Namespace); err != nil {
				return nil, fmt.Errorf("Failed to determine secret %s - %v", LIC_ADDR_SECRET, err)
			} else if secret != nil {
				if data, ok := secret.Data["license-ip"]; ok {
					licAddr = string(data)
					lic_found = true
				}
			}
			// Iterate through specified env variables and remove LIC_ENV_VAR, if secret present
			envEntries := container.Env
			var entry_found bool
			for index, env := range envEntries {
				if env.Name == LIC_ENV_VAR {
					if lic_found {
						envEntries[index].Value = licAddr
					} else {
						// License server ip through configmap
						lic_found = true
					}
					entry_found = true
					break
				}
			}
			if !entry_found && lic_found {
				envEntries = append(envEntries, corev1.EnvVar{Name: LIC_ENV_VAR, Value: licAddr})
			}
			container.Env = envEntries
		}
		if name == LICENSE_NAME {
			// Delay license server container until we confirm no secrets are present
			lic_container = container
		} else {
			log.Infof("Adding to pod: %s, container: %s, Image: %s, Args: %v, Cmd: %v, Port: %v, Vol: %v",
				CONTROLLER_NAME, name, image, container.Args, container.Command,
				container.Ports, container.VolumeMounts)
			containers = append(containers, container)
		}
	}
	if !lic_found {
		// Add license server only if secret is present or image is configmap driven
		if len(lic_container.Name) > 0 {
			log.Infof("Adding to pod: %s, container: %s, Image: %s, Args: %v, Cmd: %v, Port: %v, Vol: %v",
				CONTROLLER_NAME, LICENSE_NAME, lic_container.Image, lic_container.Args, lic_container.Command,
				lic_container.Ports, lic_container.VolumeMounts)
			containers = append(containers, lic_container)
		}
	}

	log.Infof("Done containersForController total containers %v!", len(containers))
	return containers, nil
}

func (r *IxiaTGReconciler) containersForIxia(podName string, intfList []string, ixia *networkv1beta1.IxiaTG) []corev1.Container {
	log.Infof("Get containers for Ixia: %s", podName)
	argIntfList := ""
	for _, intf := range intfList {
		argIntfList += "virtual@af_packet," + intf + " "
	}
	argIntfList = argIntfList[:len(argIntfList)-1]
	var containers []corev1.Container

	conSecurityCtx := getDefaultSecurityContext()
	versionToDeploy := latestVersion
	if ixia.Spec.Release != "" && ixia.Spec.Release != DEFAULT_VERSION {
		versionToDeploy = ixia.Spec.Release
	}
	for cName, comp := range componentDep[versionToDeploy].Ixia.Containers {
		var tcpSock corev1.TCPSocketAction
		if strings.HasPrefix(comp.Name, INIT_CONT_NAME_PREFIX) {
			continue
		}
		log.Infof("Deploying %s version %s for config version %s, ns %s (source %s)",
			cName, comp.Tag, versionToDeploy, ixia.Namespace, componentDep[versionToDeploy].Source)
		name := podName + "-" + comp.ContainerName
		image := comp.Path + ":" + comp.Tag
		container := corev1.Container{
			Name:            name,
			Image:           image,
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: conSecurityCtx,
		}
		compCopy := comp
		compCopy.DefEnv = make(map[string]string)
		for k, v := range comp.DefEnv {
			compCopy.DefEnv[k] = v
		}
		resRequest := corev1.ResourceList{}
		if r, ok := comp.MinResource["cpu"]; ok {
			resRequest["cpu"] = resource.MustParse(r)
		}
		if r, ok := comp.MinResource["memory"]; ok {
			resRequest["memory"] = resource.MustParse(r)
		}
		if cName == IMAGE_PROTOCOL_ENG {
			compCopy.DefEnv["INTF_LIST"] = strings.Join(intfList, ",")
			tcpSock = corev1.TCPSocketAction{Port: intstr.IntOrString{IntVal: PROTOCOL_ENG_PORT}}
			if _, ok := resRequest["cpu"]; !ok {
				resRequest["cpu"] = resource.MustParse(MIN_CPU_PROTOCOL)
			}
			if _, ok := resRequest["memory"]; !ok {
				min_mem := 350
				if len(intfList) > 1 {
					min_mem = 400 + (10 * len(intfList))
				}
				resRequest["memory"] = resource.MustParse(fmt.Sprintf("%vMi", min_mem))
			}
		} else {
			compCopy.DefEnv["ARG_IFACE_LIST"] = argIntfList
			tcpSock = corev1.TCPSocketAction{Port: intstr.IntOrString{IntVal: TRAFFIC_ENG_PORT}}
			if _, ok := resRequest["cpu"]; !ok {
				resRequest["cpu"] = resource.MustParse(MIN_CPU_TRAFFIC)
			}
			if _, ok := resRequest["memory"]; !ok {
				min_mem := 50 + (10 * len(intfList))
				resRequest["memory"] = resource.MustParse(fmt.Sprintf("%vMi", min_mem))
			}
		}
		container.Resources.Requests = resRequest

		if compCopy.LiveNessEnable == nil || *compCopy.LiveNessEnable {
			pbHdlr := corev1.ProbeHandler{TCPSocket: &tcpSock}
			probe := corev1.Probe{
				ProbeHandler:                  pbHdlr,
				InitialDelaySeconds:           compCopy.LiveNessDelay,
				PeriodSeconds:                 compCopy.LiveNessPeriod,
				FailureThreshold:              compCopy.LiveNessFailure,
				TerminationGracePeriodSeconds: pointer.Int64(1),
			}
			container.LivenessProbe = &probe
		}
		updateControllerContainer(&container, compCopy, false)
		log.Infof("Adding to pod: %s, container: %s, Image: %s, Args: %v, Cmd: %v, Env: %v",
			podName, name, image, container.Args, container.Command, container.Env)
		containers = append(containers, container)
	}

	log.Infof("Done containersForIxia() total containers %v!", len(containers))
	return containers
}

// SetupWithManager sets up the controller with the Manager.
func (r *IxiaTGReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1beta1.IxiaTG{}).
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

func getDefaultSecurityContext() *corev1.SecurityContext {
	var sc *corev1.SecurityContext = new(corev1.SecurityContext)
	t := true
	sc.Privileged = new(bool)
	sc.Privileged = &t
	return sc
}

func (r *IxiaTGReconciler) GetSecret(ctx context.Context, name string, namespace string) (*corev1.Secret, error) {
	instance := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, instance)
	if err == nil {
		return instance, nil
	} else if errapi.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

func (r *IxiaTGReconciler) DeleteSecrets(ctx context.Context, namespace string) error {
	secretList := []string{SECRET_NAME, LIC_ADDR_SECRET, LIC_SERVER_SECRET}
	for _, name := range secretList {
		instance := &corev1.Secret{}
		if r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, instance) == nil {
			if err := r.Delete(ctx, instance); err != nil {
				log.Errorf("Failed to delete secret %v - %v", instance, err)
				return err
			}
			log.Infof("Deleted secret %v", instance)
		}
	}
	return nil
}

func (r *IxiaTGReconciler) ReconcileSecrets(ctx context.Context,
	req ctrl.Request, ixia *networkv1beta1.IxiaTG) error {
	_ = r.Log.WithValues("ixiatg", req.NamespacedName)
	secretList := []string{SECRET_NAME, LIC_ADDR_SECRET, LIC_SERVER_SECRET}
	// Fetch the Secret instance
	instance := &corev1.Secret{}

	for _, secretName := range secretList {
		currNamespace := new(types.NamespacedName)
		currNamespace.Namespace = CURRENT_NAMESPACE
		currNamespace.Name = secretName
		log.Infof("Getting Secret for namespace : %v", *currNamespace)
		err := r.Get(ctx, *currNamespace, instance)
		if err != nil {
			if errapi.IsNotFound(err) {
				// Request object not found, could have been deleted after reconcile request.
				// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
				// Return and don't requeue
				log.Infof("No Secret found in namespace : %v", *currNamespace)
				continue
			}
			// Error reading the object - requeue the request.
			return err
		}

		if rep, ok := instance.Annotations["secretsync.ixiatg.com/replicate"]; ok && rep == "true" {
			targetSecret, err := createSecret(instance, currNamespace.Name, ixia.Namespace)
			if err != nil {
				return err
			}
			secret := &corev1.Secret{}
			err = r.Get(ctx, types.NamespacedName{Name: targetSecret.Name, Namespace: targetSecret.Namespace}, secret)
			if err != nil && errapi.IsNotFound(err) {
				log.Info(fmt.Sprintf("Target secret %s doesn't exist, creating it in namespace %s", targetSecret.Name, targetSecret.Namespace))
				err = r.Create(ctx, targetSecret)
				if err != nil {
					return err
				}
			} else {
				log.Info(fmt.Sprintf("Target secret %s exists, updating it now in namespace %s", targetSecret.Name, targetSecret.Namespace))
				err = r.Update(ctx, targetSecret)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
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
