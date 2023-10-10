#!/bin/sh

GO_VERSION=1.17.3

KNE_COMMIT=f3c905c
MESHNET_COMMIT=de89b2e
MESHNET_VERSION=v0.3.0

IXIA_C_CONTROLLER=0.0.1-2897
IXIA_C_PROTOCOL_ENGINE=""
IXIA_C_TRAFFIC_ENGINE=""
IXIA_C_GRPC_SERVER=""
IXIA_C_GNMI_SERVER=""
ARISTA_CEOS_VERSION=4.28.01F
IXIA_C_TEST_CLIENT=0.0.1-1344

OLD_TOPO_SUPPORTED_VERSION=0.0.1-2678

# source path for current session
. $HOME/.profile

if [ "$(id -u)" -eq 0 ] && [ -n "$SUDO_USER" ]
then
    echo "This script should not be run as sudo"
    exit 1
fi

# get installers based on host architecture
if [ "$(arch)" = "aarch64" ] || [ "$(arch)" = "arm64" ]
then
    echo "Host architecture is ARM64"
    GO_TARGZ=go${GO_VERSION}.linux-arm64.tar.gz
elif [ "$(arch)" = "x86_64" ]
then
    echo "Host architecture is x86_64"
    GO_TARGZ=go${GO_VERSION}.linux-amd64.tar.gz
else
    echo "Host architecture $(arch) is not supported"
    exit 1
fi

# Avoid warnings for non-interactive apt-get install
export DEBIAN_FRONTEND=noninteractive

cecho() {
    echo "\n\033[1;32m${1}\033[0m\n"
}

get_cluster_deps() {
    sudo apt-get update \
    && sudo apt-get install -y --no-install-recommends curl git vim apt-transport-https ca-certificates gnupg lsb-release
}

get_go() {
    go version 2> /dev/null && return
    cecho "Installing Go ..."
    # install golang per https://golang.org/doc/install#tarball
    curl -kL https://dl.google.com/go/${GO_TARGZ} | sudo tar -C /usr/local/ -xzf - \
    && echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> $HOME/.profile \
    && . $HOME/.profile \
    && go version
}

get_docker() {
    sudo docker version 2> /dev/null && return
    cecho "Installing docker ..."
    sudo apt-get remove docker docker-engine docker.io containerd runc 2> /dev/null

    curl -kfsSL https://download.docker.com/linux/ubuntu/gpg \
        | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg

    echo "deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
        | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null


    # hack to not detect MitM when a corporate proxy is sitting in between
    conf=/etc/apt/apt.conf.d/99docker-skip-cert-verify.conf
    curl -fsL https://download.docker.com/linux/ubuntu/gpg 2>&1 > /dev/null \
        || echo "Acquire { https::Verify-Peer false }" | sudo tee "$conf" \
        && sudo mkdir -p /etc/docker \
        && echo '{ "registry-mirrors": ["https://docker-remote.artifactorylbj.it.keysight.com"] }' | sudo tee -a /etc/docker/daemon.json
    
    sudo apt-get update \
    && sudo apt-get install -y docker-ce docker-ce-cli containerd.io || exit 1
    # partially undo hack
    sudo rm -rf "$conf"
    # remove docker.list from apt-get if hack is applied (otherwise apt-get update will fail)
    curl -fsL https://download.docker.com/linux/ubuntu/gpg 2>&1 > /dev/null \
        || sudo rm -rf /etc/apt/sources.list.d/docker.list

    sudo docker version
}

use_docker_without_sudo() {
    docker version 2> /dev/null && return

    cecho "Adding $USER to group docker"
    # use docker without sudo
    sudo groupadd docker 2> /dev/null || true
    sudo usermod -aG docker $USER

    cecho "Please logout, login and execute previously entered command again !"
    exit 0
}

get_kind() {
    go install sigs.k8s.io/kind@v0.11.1
}

get_kubectl() {
    cecho "Copying kubectl from kind cluster to host ..."
    docker cp kind-control-plane:/usr/bin/kubectl $HOME/go/bin/
}

get_kne() {
    cecho "Getting kne commit: $KNE_COMMIT ..."
    rm -rfv kne 
    git clone https://github.com/google/kne.git
    cd kne 
    git checkout $KNE_COMMIT 
    cd -
    cd kne/kne_cli && go install && cd -
    rm -rf kne
}

gcloud_auth() {
    gcloud auth application-default login --no-launch-browser
    gcloud auth configure-docker --quiet us-central1-docker.pkg.dev
}

get_gcloud() {
    gcloud version 2>/dev/null && return
    cecho "Setting up gcloud"
    dl=google-cloud-sdk-349.0.0-linux-x86_64.tar.gz
    cd $HOME
    curl -kLO https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/${dl}
    tar xzvf $dl && rm -rf $dl
    cd -
    echo 'export PATH=$PATH:$HOME/google-cloud-sdk/bin' >> $HOME/.profile
    # source path for current session
    . $HOME/.profile

    gcloud init
}

wait_for_all_pods_to_be_ready() {
    for n in $(kubectl get namespaces -o 'jsonpath={.items[*].metadata.name}')
    do
        if [ "${1}" = "-ns" ] && [ "${2}" != "${n}" ]
        then
            continue
        fi
        for p in $(kubectl get pods -n ${n} -o 'jsonpath={.items[*].metadata.name}')
        do
            cecho "Waiting for pod/${p} in namespace ${n} (timeout=300s)..."
            kubectl wait -n ${n} pod/${p} --for condition=ready --timeout=300s
        done
    done

    cecho "Pods:"
    kubectl get pods -A
    cecho "Services:"
    kubectl get services -A
}

wait_for_pod_counts() {
    namespace=${1}
    count=${2}
    start=$SECONDS
    while true
    do
        echo "Waiting for all pods to be expected under namespace ${1}..."
        
        echo "Expected Pods ${2}"
        pod_count=$(kubectl get pods -n ${1} --no-headers 2> /dev/null | wc -l)
        echo "Actual Pods ${pod_count}"
        # if expected pod count is 0, then check that actual count is 0 as well
        if [ "${2}" = 0 ] && [ "${pod_count}" = 0 ]
        then
            break
        else if [ "${2}" -gt 0 ] && [ "${pod_count}" -gt 0 ]
        then
            # if expected pod count is more than 0, then ensure actual count is more than 0 as well
            break
        fi
        fi

        elapsed=$(( SECONDS - start ))
        if [ $elapsed -gt 300 ]
        then
            echo "All pods are not as expected under namespace ${1} with 300 seconds"
            exit 1
        fi
        sleep 0.5
    done

    cecho "Pods:"
    kubectl get pods -A
}

get_meshnet() {
    cecho "Getting meshnet-cni commit: $MESHNET_COMMIT ..."
    rm -rf meshnet-cni && git clone https://github.com/networkop/meshnet-cni \
    && cd meshnet-cni \
    && git checkout $MESHNET_COMMIT \
    && sed -i "s/^\s*image\:\ networkop\/meshnet\:latest.*/          image\:\ networkop\/\meshnet\:$MESHNET_VERSION/g" ./manifests/base/daemonset.yaml \
    && kubectl apply -k manifests/base \
    && wait_for_pod_counts meshnet 1 \
    && wait_for_all_pods_to_be_ready -ns meshnet \
    && cd - \
    && rm -rf meshnet-cni
}

get_metallb() {
    cecho "Getting metallb ..."
    kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.12/manifests/namespace.yaml \
    && kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)" \
    && kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.12/manifests/metallb.yaml \
    && wait_for_pod_counts metallb-system 1 \
    && wait_for_all_pods_to_be_ready -ns metallb-system || exit 1

    prefix=$(docker network inspect -f '{{.IPAM.Config}}' kind | grep -Eo "[0-9]+\.[0-9]+\.[0-9]+" | tail -n 1)

    yml=metallb-config
    sed -e "s/\${prefix}/${prefix}/g" ${yml}.template.yaml > ${yml}.yaml \
    && cecho "Applying metallb config map for exposing internal services via public IP addresses ..." \
    && cat ${yml}.yaml \
    && kubectl apply -f ${yml}.yaml
}

rm_meshnet() {
    cecho "Getting meshnet-cni commit: $MESHNET_COMMIT ..."
    rm -rf meshnet-cni && git clone https://github.com/networkop/meshnet-cni
    cd meshnet-cni && git checkout $MESHNET_COMMIT
    sed -i "s/^\s*image\:\ networkop\/meshnet\:latest.*/          image\:\ networkop\/\meshnet\:${MESHNET_VERSION}/g" ./manifests/base/daemonset.yaml
    kubectl delete -k manifests/base
    cd -
}

rm_kind_cluster() {
    kind delete cluster 2> /dev/null
    rm -rf $HOME/.kube
    rm -rf $HOME/go/bin/kubectl
}

install_kind_container_deps() {
    docker exec -t kind-control-plane apt-get update
    docker exec -t kind-control-plane apt-get install openssh-server
}

setup_kind_cluster() {
    rm_kind_cluster \
    && kind create cluster --wait 5m \
    && get_kubectl \
    && get_meshnet \
    && get_metallb \
    && wait_for_all_pods_to_be_ready \
    && install_kind_container_deps
}

setup_cluster() {
    get_cluster_deps \
    && get_docker \
    && use_docker_without_sudo \
    && get_go \
    && get_kind \
    && setup_kind_cluster
}

setup() {
    setup_cluster \
    && get_gcloud \
    && gcloud_auth
}

get_release_configmap() {
    echo "Downloading ixia-configmap.yaml for Ixia-C ${IXIA_C_CONTROLLER}..."
    curl -kLO "https://github.com/open-traffic-generator/ixia-c/releases/download/v${IXIA_C_CONTROLLER}/ixia-configmap.yaml"
}

load_ixia_c_images() {
    IMG=""
    TAG=""
    get_release_configmap
    yml=./ixia-configmap.yaml
    cecho "Loading container images for Ixia-c from ${yml} ..."

    while read line
    do
        if [ -z "${IMG}" ]
        then
            IMG=$(echo "$line" | grep path | cut -d\" -f4)
        elif [ -z "${TAG}" ]
        then
            TAG=$(echo "$line" | grep tag | cut -d\" -f4)
        else
            PTH="$IMG:$TAG"
            IMG=""
            TAG=""

            cecho "Loading $PTH"
            docker pull $PTH
            kind load docker-image $PTH
        fi
    done <${yml}
    rm -rf ${yml}
}

load_ixia_c_test_client_image() {
    PTH=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ixia-c-test-client:${IXIA_C_TEST_CLIENT}
    cecho "Loading $PTH"
    docker pull $PTH
    kind load docker-image $PTH
}

load_ceos_image() {
    PTH=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ceos:${ARISTA_CEOS_VERSION}
    cecho "Loading $PTH"
    docker pull $PTH
    kind load docker-image $PTH
}

load_ixia_c_operator_image() {
    cecho "Loading ixia-c operator image ..."
    img="keng-operator.tar"
    cecho "Loading ${img} ..."
    if [ ! -f "$img" ]
    then
        gunzip "${img}.gz" 2> /dev/null || true
    fi
    kind load image-archive "${img}"
}

load_older_topo_supported_controller() {
    LATEST_PTH=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ixia-c-controller:${IXIA_C_CONTROLLER}
    cecho "Loading $LATEST_PTH"
    docker pull $LATEST_PTH

    cecho "Loading $PTH"
    PTH=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ixia-c-controller:${OLD_TOPO_SUPPORTED_VERSION}
    docker tag $LATEST_PTH $PTH
    kind load docker-image $PTH
}

load_images() {
    load_ixia_c_images \
    && load_ixia_c_test_client_image \
    && load_ceos_image \
    && load_ixia_c_operator_image \
    && load_older_topo_supported_controller
}

delete_ixia_c_images() {
    IMG=""
    TAG=""
    get_release_configmap
    yml=./ixia-configmap.yaml
    cecho "Loading container images for Ixia-c from ${yml} ..."

    while read line
    do
        if [ -z "${IMG}" ]
        then
            IMG=$(echo "$line" | grep path | cut -d\" -f4)
        elif [ -z "${TAG}" ]
        then
            TAG=$(echo "$line" | grep tag | cut -d\" -f4)
        else
            PTH="$IMG:$TAG"
            cecho "Deleting $PTH"
            docker rmi -f $PTH 2> /dev/null || true
            docker exec -t kind-control-plane crictl rmi $(docker exec -t kind-control-plane crictl images | grep "$IMG") 2> /dev/null || true
            IMG=""
            TAG=""
            
        fi
    done <${yml}
    rm -rf ${yml}
}

delete_ixia_c_test_client_image() {
    IMG=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ixia-c-test-client
    PTH=${IMG}:${IXIA_C_TEST_CLIENT}
    cecho "Deleting $PTH"
    docker rmi -f $PTH 2> /dev/null || true
    docker exec -t kind-control-plane crictl rmi $(docker exec -t kind-control-plane crictl images | grep "$IMG") 2> /dev/null || true
}

delete_ceos_image() {
    IMG=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ceos
    PTH=$IMG:${ARISTA_CEOS_VERSION}
    cecho "Deleting $PTH"
    docker rmi -f $PTH 2> /dev/null || true
    docker exec -t kind-control-plane crictl rmi $(docker exec -t kind-control-plane crictl images | grep "$IMG") 2> /dev/null || true
}

delete_ixia_c_operator_image() {
    cecho "Deleting ixia-c operator image ..."
    docker exec -t kind-control-plane crictl rmi $(docker exec -t kind-control-plane crictl images | grep "ixia-c-operator") 2> /dev/null || true
}

delete_images() {
    delete_ixia_c_images \
    && delete_ixia_c_test_client_image \
    && delete_ceos_image \
    && delete_ixia_c_operator_image
}

get_component_versions() {
    echo "Downloading versions.yaml for Ixia-C ${IXIA_C_CONTROLLER}..."
    curl -kLO "https://github.com/open-traffic-generator/ixia-c/releases/download/v${IXIA_C_CONTROLLER}/versions.yaml"
    echo "Getting Coponents versions from versions.yaml..."
    cat versions.yaml
    echo ""
    IXIA_C_PROTOCOL_ENGINE=$(cat versions.yaml | grep "ixia-c-protocol-engine: " | sed -n 's/^\ixia-c-protocol-engine: //p' | tr -d '[:space:]')
    echo "Ixia-C protocol engine version : ${IXIA_C_PROTOCOL_ENGINE}"
    IXIA_C_TRAFFIC_ENGINE=$(cat versions.yaml | grep "ixia-c-traffic-engine: " | sed -n 's/^\ixia-c-traffic-engine: //p' | tr -d '[:space:]')
    echo "Ixia-C traffic engine version : ${IXIA_C_TRAFFIC_ENGINE}"
    IXIA_C_GRPC_SERVER=$(cat versions.yaml | grep "ixia-c-grpc-server: " | sed -n 's/^\ixia-c-grpc-server: //p' | tr -d '[:space:]')
    echo "Ixia-C gRPC server version : ${IXIA_C_GRPC_SERVER}"
    IXIA_C_GNMI_SERVER=$(cat versions.yaml | grep "ixia-c-gnmi-server: " | sed -n 's/^\ixia-c-gnmi-server: //p' | tr -d '[:space:]')
    echo "Ixia-C gnmi server version : ${IXIA_C_GNMI_SERVER}"
    echo "Arista ceos version : ${ARISTA_CEOS_VERSION}"
    echo "Ixia-C test client version : ${IXIA_C_TEST_CLIENT}"
    rm -rf versions.yaml
}

wait_for_svc_counts() {
    namespace=${1}
    count=${2}
    start=$SECONDS
    while true
    do
        cecho "Waiting for all services to be as expected under namespace ${namespace}..."
        cecho "Expected Services ${count}"
        svc_count=$(kubectl get svc -n ${namespace} | wc -l | tr -d $'\r' | bc -l)
        if [ $svc_count -gt 0 ]
        then
            svc_count="$(($svc_count-1))"
        fi
        cecho "Actual Services ${svc_count}"
        kubectl get svc -A
        if [ ${svc_count} = ${count} ]
        then 
            cecho "All services expected under namespace ${namespace}..."
            break
        fi
        elapsed=$(( SECONDS - start ))
        if [ $elapsed -gt 300 ]
        then
            cecho "All services are not as expected under namespace ${namespace} with 300 seconds"
            exit 1
        fi
        sleep 0.5
    done
}

deploy_ixia_c_operator() {
    cecho "Deploying ixia-c operator ..."
    kubectl apply -f ixiatg-operator.yaml
    wait_for_pod_counts ixiatg-op-system 1
    wait_for_svc_counts ixiatg-op-system 1
    wait_for_all_pods_to_be_ready
}

deploy_ixia_c_test_client() {
    cecho "Deploying ixia-c test client ..."
    cat ./template-ixia-c-test-client.yaml | \
        sed "s/IXIA_C_TEST_CLIENT/${IXIA_C_TEST_CLIENT}/g" | \
        tee ./ixia-c-test-client.yaml > /dev/null
    kubectl apply -f ixia-c-test-client.yaml
    wait_for_pod_counts default 1
    wait_for_all_pods_to_be_ready
    rm -rf ixia-c-test-client.yaml
}

deploy_ixia_configmap() {
    LATEST_RELEASE=local-latest
    get_component_versions
    cecho "Deploying local-latest ixia-configmap..."
    cat ./template-ixia-configmap.yaml | \
        sed "s/LATEST_RELEASE/${LATEST_RELEASE}/g" | \
        sed "s/IXIA_C_CONTROLLER_VERSION/${IXIA_C_CONTROLLER}/g" | \
        sed "s/IXIA_C_GNMI_SERVER_VERSION/${IXIA_C_GNMI_SERVER}/g" | \
        sed "s/IXIA_C_GRPC_SERVER_VERSION/${IXIA_C_GRPC_SERVER}/g" | \
        sed "s/IXIA_C_TRAFFIC_ENGINE_VERSION/${IXIA_C_TRAFFIC_ENGINE}/g" | \
        sed "s/IXIA_C_PROTOCOL_ENGINE_VERSION/${IXIA_C_PROTOCOL_ENGINE}/g" | \
        tee ./ixia-configmap.yaml > /dev/null
    kubectl apply -f ixia-configmap.yaml
    rm -rf ixia-configmap.yaml
}

deploy_minimal_topo() {
    cecho "Deploying minimal topology...."
    kubectl delete namespace ixia-c
    $HOME/go/bin/kne_cli create minimal_topo.txt
    wait_for_pod_counts ixia-c 3
    wait_for_all_pods_to_be_ready

    cecho "Deleting minimal topology...."
    $HOME/go/bin/kne_cli delete minimal_topo.txt
    wait_for_pod_counts ixia-c 0
}

deploy_old_topo_configmap() {
    OLD_RELEASE=local-old
    get_component_versions
    cecho "Deploying local-old ixia-configmap..."
    cat ./template-ixia-configmap.yaml | \
        sed "s/LATEST_RELEASE/${OLD_RELEASE}/g" | \
        sed "s/IXIA_C_CONTROLLER_VERSION/${OLD_TOPO_SUPPORTED_VERSION}/g" | \
        sed "s/IXIA_C_GNMI_SERVER_VERSION/${IXIA_C_GNMI_SERVER}/g" | \
        sed "s/IXIA_C_GRPC_SERVER_VERSION/${IXIA_C_GRPC_SERVER}/g" | \
        sed "s/IXIA_C_TRAFFIC_ENGINE_VERSION/${IXIA_C_TRAFFIC_ENGINE}/g" | \
        sed "s/IXIA_C_PROTOCOL_ENGINE_VERSION/${IXIA_C_PROTOCOL_ENGINE}/g" | \
        tee ./ixia-configmap.yaml > /dev/null
    kubectl apply -f ixia-configmap.yaml
    rm -rf ixia-configmap.yaml
    kubectl delete namespace ixia-c 2> /dev/null || true
}

deploy() {
    deploy_ixia_c_test_client \
    && deploy_ixia_c_operator \
    && get_kne \
    && deploy_ixia_configmap \
    && deploy_minimal_topo \
    && deploy_old_topo_configmap
}

delete_ixia_c_test_client() {
    cecho "Deleting ixia-c test client ..."
    cat ./template-ixia-c-test-client.yaml | \
        sed "s/IXIA_C_TEST_CLIENT/${IXIA_C_TEST_CLIENT}/g" | \
        tee ./ixia-c-test-client.yaml > /dev/null
    kubectl delete -f ixia-c-test-client.yaml
    wait_for_pod_counts default 0
    rm -rf ixia-c-test-client.yaml
}

delete_ixia_configmap() {
    get_component_versions
    cecho "Deleting ixia-configmap..."
    cat ./template-ixia-configmap.yaml | \
        sed "s/IXIA_C_CONTROLLER_VERSION/${IXIA_C_CONTROLLER}/g" | \
        sed "s/IXIA_C_GNMI_SERVER_VERSION/${IXIA_C_GNMI_SERVER}/g" | \
        sed "s/IXIA_C_GRPC_SERVER_VERSION/${IXIA_C_GRPC_SERVER}/g" | \
        sed "s/IXIA_C_TRAFFIC_ENGINE_VERSION/${IXIA_C_TRAFFIC_ENGINE}/g" | \
        sed "s/IXIA_C_PROTOCOL_ENGINE_VERSION/${IXIA_C_PROTOCOL_ENGINE}/g" | \
        tee ./ixia-configmap.yaml > /dev/null
    kubectl delete -f ixia-configmap.yaml
    rm -rf ixia-configmap.yaml
}

delete_ixia_c_operator() {
    cecho "Deleting ixia-c operator ..."
    kubectl delete -f ixiatg-operator.yaml
    wait_for_pod_counts ixiatg-op-system 0
    wait_for_svc_counts ixiatg-op-system 0
}

delete() {
    delete_ixia_c_test_client \
    && delete_ixia_configmap \
    && delete_ixia_c_operator
}

deploy_operator_tests() {
    cecho "Deploying ixia-c operator tests ..." 
    rm -rf operator-tests 2> /dev/null || true
    tar -xvf operator-tests.tar.gz
    python3 -m pip install -r ./operator-tests/py/requirements.txt
    cp ixia-configmap.yaml ./operator-tests/
}



case $1 in
    *   )
        # shift positional arguments so that arg 2 becomes arg 1, etc.
        cmd=${1}
        shift 1
        ${cmd} ${@} || cecho "usage: $0 [name of any function in script]"
    ;;
esac
