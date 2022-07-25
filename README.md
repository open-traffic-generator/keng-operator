# Ixia-C Operator

[![Project Status: Active – The project has reached a stable, usable state and is being actively developed.](https://www.repostatus.org/badges/latest/active.svg)](https://www.repostatus.org/#active)
[![license](https://img.shields.io/badge/license-MIT-green.svg)](https://en.wikipedia.org/wiki/MIT_License)
[![release)](https://img.shields.io/github/v/release/open-traffic-generator/ixia-c-operator)](https://github.com/open-traffic-generator/ixia-c-operator/releases/latest)
[![Build](https://github.com/open-traffic-generator/ixia-c-operator/actions/workflows/publish.yaml/badge.svg)](https://github.com/open-traffic-generator/ixia-c-operator/actions)
[![LGTM Grade](https://img.shields.io/lgtm/grade/python/github/open-traffic-generator/ixia-c-operator)](https://lgtm.com/projects/g/open-traffic-generator/ixia-c-operator/context:python)
[![LGTM Alerts](https://img.shields.io/lgtm/alerts/github/open-traffic-generator/ixia-c-operator)](https://lgtm.com/projects/g/open-traffic-generator/ixia-c-operator/?mode=list)

Kubernetes Operator is built on the basic Kubernetes resources and controller concepts and includes application specific knowledge to automate common tasks like create, configure and manage instances on behalf of a Kubernetes user. It extends the functionality of the Kubernetes API and is used to package, deploy and manage Kubernetes application.<br/>
    
Ixia Operator defines CRD for Ixia network device ([IxiaTG](#ixiatg-crd)) and can be used to build up different network topologies with network devices from other vendors. Network interconnects between the topology nodes can be setup with various container network interface (CNI) plugins for Kubernetes for attaching multiple network interfaces to the nodes.<br/>

This process happens in two phases; in the first phase the operator identifies the network interconnects that needs to be setup externally. In the second phase, once the network interconnects are setup, the operator deploys the containers and services. This entire process has been much simplified with the use of [KNE](https://github.com/google/kne). It automates this process and enables us to setup network topologies in Kubernetes. KNE uses [Meshnet](https://github.com/networkop/meshnet-cni) CNI to setup the network interconnects. Ixia Operator watches out for IxiaTG CRDs to be instantiated in Kubernetes environment and accordingly initiates Ixia specific resource management.<br/>
    
The various Ixia component versions to be deployed is derived from the Ixia release version as specified in the IxiaTG config. These component mappings are captured in ixia-configmap.yaml for each Ixia release. The configmap, as shown in the snippet below, comprise of the Ixia release version ("release"), and the list of qualified component versions, for that release. Ixia Operator first tries to access these details from Keysight published releases; if unable to so, it tries to locate them in Kubernetes configmap. This allows users to have the operator load images from private repositories, by updating the configmap entries. Thus, for deployment with custom images, the user is expected to download release specific ixia-configmap.yaml from published [releases](https://github.com/open-traffic-generator/ixia-c/releases/). Then, in the configmap, update the specific container image "path" / "tag" fields and also update the "release" to some custom name. Start the operator first as specified in the deployment section below, before applying the configmap locally. After this the operator can be used to deploy the containers and services.<br/><br/>

```sh
  "release": "local-latest",
  "images": [
      {
          "name": "controller",
          "path": "ixiacom/ixia-c-controller",
          "tag": "0.0.1-2994"
      },
      {
          "name": "gnmi-server",
          "path": "ixiacom/ixia-c-gnmi-server",
          "tag": "1.8.3"
      },
      {
          "name": "traffic-engine",
          "path": "docker-local-ixvm-lbj.artifactorylbj.it.keysight.com/athena-traffic-engine",
          "tag": "1.4.1.29"
      },
      {
```

If some of the required images are not publicly available and distributed under license, user would be required to create a secret with docker authorization details as supplied by Keysight. The operator would refer to this secret for deploying from private repository.<br/><br/>

<kbd> <img src="Ixia_Operator.jpg"> </kbd><br/><br/>

The operator deploys one single Controller pod with Ixia-c, gNMI and gRPC containers for user control, management and statistics reporting of Ixia network devices. It also deploys Ixia network device nodes for control and data plane. The deployed Ixia resource release versions are anchored and dictated by the Ixia-c release as defined in the KNE config file.

## IxiaTG CRD

The IxiaTG CRD instance specifies the list of Ixia components to be deployed. These deployment details are captured in the CRD "spec" and comprise of the following fields.
- Release - Ixia release specific components version to deploy
- Desired State - specify phase of deployment either INITIATED or DEPLOYED
- Api Endpoint Map - service end points for control and management of all Ixia nodes in the topology
- Interfaces - the Ixia list of interfaces and groups in the topology

In the first phase of deployment (desired state set to INITIATED), the operator determines the pod names and their interfaces that it will deploy in the second phase. It updates these details in the "status" component of the CRD instance, the "state" is also updated as specified in the "spec" desired state. The CRD instance "status" comprise of the following fields.
- State - status of the operation, either as specified in desired state or FAILED
- Reason - error message on failure
- Api Endpoint - generated service names for reference
- Interfaces - list of interface mappings with pod name and interface name

Based on these details, once the mesh of interconnects are setup, the Ixia CRD instance is updated with "spec" desired state set to DEPLOYED to trigger the pod and services deployment phase to start in the operator. On successful deployment the operator again updates the "status" state component to DEPLOYED. On failure state is set to FAILED and reason is updated with to error message. Below is an example of CRD instance.

```sh
spec:
  api_endpoint_map:
    gnmi:
      in: 50051
    grpc:
      in: 40051
    http:
      in: 443
  desired_state: DEPLOYED
  interfaces:
  - name: eth1
  - group: lag
    name: eth2
  - group: lag
    name: eth3
  release: local-latest
status:
  api_endpoint:
    pod_name: otg-controller
    service_names:
    - service-gnmi-otg-controller
    - service-grpc-otg-controller
    - service-http-otg-controller
  interfaces:
  - interface: eth1
    name: eth1
    pod_name: otg-port-eth1
  - interface: eth2
    name: eth2
    pod_name: otg-port-group-lag
  - interface: eth3
    name: eth3
    pod_name: otg-port-group-lag
  state: DEPLOYED
```

## Deployment

Please make sure that the setup meets [Deployment Prerequisites](#deployment-prerequisites).

- **Available Releases**
    https://github.com/open-traffic-generator/ixia-c-operator/releases

- **Download Deployment yaml**

  ```sh
  curl -kLO "https://github.com/open-traffic-generator/ixia-c-operator/releases/tag/v0.0.65/ixiatg-operator.yaml"
  ```

- **Load Image**

  ```sh
  docker pull ixiacom/ixia-c-operator:0.0.65
  ```

- **Running as K8S Pod**

  ```sh
  kubectl apply -f ixiatg-operator.yaml
  ```

## Deployment Prerequisites

- Please make sure you have kubernetes cluster up in your setup.

## Build

- **Clone this project**

  ```sh
  git clone https://github.com/open-traffic-generator/ixia-c-operator.git
  cd ixia-c-operator/
  ```

- **For Production**

    ```sh
    export VERSION=latest
    export IMAGE_TAG_BASE=ixia-c-operator

    # Generating ixia-c-operator deployment yaml using Makefile
    make yaml
    # Generating docker build with name & tag (ixia-c-operator:latest) using Makefile
    make docker-build
    ```

- **For Development**

    ```sh
    # after cloning the repo, some dependencies need to get installed for further development
    chmod u+x ./do.sh
    ./do.sh deps
    ```

## Quick Tour

**do.sh** covers most of what needs to be done manually. If you wish to extend it, just define a function (e.g. install_deps()) and call it like so: `./do.sh install_deps`.

```sh

# install dependencies
./do.sh deps
# build production docker image
./do.sh build
# generate production yaml for operator deployment
./do.sh yaml
```

## Test Changes

TBD




