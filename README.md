# Ixia-C Operator

TBD: Describe briefly what operator does

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