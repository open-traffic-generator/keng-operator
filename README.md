# Ixia-C Operator

Kubernetes Operator is built on the basic Kubernetes resources and controller concepts and includes application specific knowledge to automate common tasks like create, configure and manage instances on behalf of a Kubernetes user. It extends the functionality of the Kubernetes API and is used to package, deploy and manage Kubernetes application.


Ixia Operator defines CRD for Ixia network device (IxiaTG) and can be used to build up different network topologies possibly with network devices from other vendors. Network interconnects between the topology nodes can be setup with various container network interface (CNI) plugins for Kubernetes for attaching multiple network interfaces to the nodes.


[KNE](https://github.com/google/kne) automates this process and enables us to setup network topologies in Kubernetes. It uses [Meshnet](https://github.com/networkop/meshnet-cni) CNI to setup the network interconnects. Ixia Operator watches out for IxiaTG CRDs to be instantiated in Kubernetes environment and accordingly initiates Ixia specific resource management. Based on the node version in the IxiaTG config, it first tries to access the deployment resource mapping details from Keysight published [releases](https://github.com/open-traffic-generator/ixia-c/releases/); in case of failure or in absence of internet connectivity, it refers to local configmap. This way it allows user to deploy custom versions by allowing load of a configmap with custom updates and then specifying that custom version in KNE config file. If some of the required images are not publicly available and distributed under license, user would be required to create a secret with docker authorization details as supplied by Keysight. The operator would refer to this secret for deploying from private repository.


<img src="Ixia_Operator.jpg">



The operator deploys one single Controller pod with Ixia-c, gNMI and gRPC containers for user control, management and stats report of Ixia network devices; and Ixia network devices for control and data plane. The deployed Ixia resource release versions are anchored and dictated by the Ixia-c release as defined in the KNE config file.


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
  https://github.com/open-traffic-generator/ixia-c-operator.git
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
