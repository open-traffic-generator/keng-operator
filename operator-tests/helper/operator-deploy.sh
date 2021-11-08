#!/bin/sh

export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin

set -e
ARGS="${@}"

KNE_COMMIT=8c56d03
ARISTA_CEOS_VERSION=4.26.1F

KIND_SINGLE_NODE_NAME="kind-control-plane"

cecho() {
    echo "\n\033[1;32m${1}\033[0m\n"
}

install_go() {
    go version 2> /dev/null && return
    cecho "Installing Go ..."
    # install golang per https://golang.org/doc/install#tarball
    curl -kL https://dl.google.com/go/go1.16.5.linux-amd64.tar.gz | sudo tar -C /usr/local/ -xzf -
    echo "export PATH=\$PATH:/usr/local/go/bin" >> $HOME/.profile
    # source path for current session
    . $HOME/.profile
    # TODO: set GOPATH ?
    # echo "export GOPATH=/home/go" >> $HOME/.profile
}

get_kne() {
    cecho "Getting kne commit: $KNE_COMMIT ..."
    rm -rfv kne && git clone https://github.com/google/kne
    cd kne && git checkout $KNE_COMMIT && cd -
    cd kne/kne_cli && go build && cd -
}

load_ixia_c_images() {
    cecho "Loading ixia-c images ..."
    docker rmi -f $(docker images | grep 'ceos') 2> /dev/null || true
    for i in ixia-c-controller ixia-c-traffic-engine ixia-c-protocol-engine ixia-c-grpc-server ixia-c-gnmi-server ixia-c-test-client
    do
        img="${i}.tar"
        cecho "Loading ${img} ..."
        gunzip "${img}.gz" 2> /dev/null || true
        kind load image-archive "${img}"
    done
}

load_operator_image() {
    cecho "Loading ixia-c operator image ..."
    img="ixia-c-operator.tar"
    cecho "Loading ${img} ..."
    gunzip "${img}.gz" 2> /dev/null || true
    kind load image-archive "${img}"
}

load_ceos_image() {
    cecho "Downloading arista cEOS image for version ${ARISTA_CEOS_VERSION} ..."
    img="cEOS64-lab-${ARISTA_CEOS_VERSION}.tar"
    cecho "Loading ${img} ..."
    docker rmi -f $(docker images | grep '<none>') 2> /dev/null || true
    docker rmi -f $(docker images | grep 'ceos') 2> /dev/null || true
    sudo docker import "${img}" "ceos:${ARISTA_CEOS_VERSION}"
    kind load docker-image "ceos:${ARISTA_CEOS_VERSION}"
    docker rmi -f $(docker images | grep '<none>') 2> /dev/null || true
}

load_images() {
    delete_images
    load_ceos_image
    load_ixia_c_images
    load_operator_image
}

delete_images() {
    docker exec -t $KIND_SINGLE_NODE_NAME crictl rmi $(docker exec -t $KIND_SINGLE_NODE_NAME crictl images | grep 'ixia-c') 2> /dev/null || true
    docker exec -t $KIND_SINGLE_NODE_NAME crictl rmi $(docker exec -t $KIND_SINGLE_NODE_NAME crictl images | grep 'us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ixia-c') 2> /dev/null || true
    docker exec -t $KIND_SINGLE_NODE_NAME crictl rmi $(docker exec -t $KIND_SINGLE_NODE_NAME crictl images | grep '<none>') 2> /dev/null || true
    docker exec -t $KIND_SINGLE_NODE_NAME crictl rmi $(docker exec -t $KIND_SINGLE_NODE_NAME crictl images | grep 'ceos') 2> /dev/null || true
    docker rmi -f $(docker images | grep 'ixia-c') 2> /dev/null || true
    docker rmi -f $(docker images | grep 'us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight/ixia-c') 2> /dev/null || true
    docker rmi -f $(docker images | grep '<none>') 2> /dev/null || true
    docker rmi -f $(docker images | grep 'ceos') 2> /dev/null || true
}

wait_for_pod_counts() {
    namespace=${1}
    count=${2}
    start=$SECONDS
    while true
    do
        cecho "Waiting for all pods to be expected under namespace ${namespace}..."
        cecho "Expected Pods ${count}"
        pod_count=$(kubectl get pods -n ${namespace} | wc -l | tr -d $'\r' | bc -l)
        if [ $pod_count -gt 0 ]
        then
            pod_count="$(($pod_count-1))"
        fi
        cecho "Actual Pods ${pod_count}"
        kubectl get pods -A
        if [ ${pod_count} = ${count} ]
        then
            cecho "All pods created under namespace ${namespace}..."
            break
        fi
        elapsed=$(( SECONDS - start ))
        if [ $elapsed -gt 300 ]
        then
            cecho "All pods are not as expected under namespace ${namespace} with 300 seconds"
            exit 1
        fi
        sleep 0.5
    done
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

wait_for_all_pods_to_be_ready() {
    for n in $(kubectl get namespaces -o 'jsonpath={.items[*].metadata.name}')
    do
        for p in $(kubectl get pods -n ${n} -o 'jsonpath={.items[*].metadata.name}')
        do
            cecho "Waiting for pod/${p} in namespace ${n} (timeout=300s)..."
            kubectl wait -n ${n} pod/${p} --for condition=ready --timeout=300s
            kubectl wait -n ${n} pod/${p} --for condition=containersready --timeout=300s
        done
    done
}

deploy_ixia_c_operator() {
    cecho "Deploying ixia-c operator ..."
    kubectl taint node master node-role.kubernetes.io/master:NoSchedule- 2> /dev/null || true
    kubectl apply -f ixiatg-operator.yaml
    kubectl taint node master node-role.kubernetes.io/master:NoSchedule- 2> /dev/null || true
    wait_for_pod_counts ixiatg-op-system 1
    wait_for_svc_counts ixiatg-op-system 1
    wait_for_all_pods_to_be_ready
}

deploy_ixia_c_test_client() {
    cecho "Deploying ixia-c test client ..."
    kubectl apply -f ixia-c-test-client.yaml
    wait_for_pod_counts default 1
    wait_for_all_pods_to_be_ready
}

deploy_ixia_configmap() {
    cecho "Deploying ixia-configmap..."
    kubectl apply -f ixia-configmap.yaml
}

copy_scripts() {
    cecho "Copy all yaml files..."
    for i in ixia-configmap.yaml ixiatg-operator.yaml ixia-c-test-client.yaml
    do
    	docker cp ${i} $KIND_SINGLE_NODE_NAME:/
    done
    cecho "Copy automated deployment script..."
    docker cp ./operator-deploy.sh $KIND_SINGLE_NODE_NAME:/
}


deploy() {
    deploy_ixia_c_operator
    deploy_ixia_configmap
    deploy_ixia_c_test_client
}

deploy_in_kind() {
    copy_scripts
    docker exec -t $KIND_SINGLE_NODE_NAME /bin/bash -c "chmod u+x ./operator-deploy.sh"
    docker exec -t $KIND_SINGLE_NODE_NAME /bin/bash -c "./operator-deploy.sh deploy"
}

deploy_tests() {
    cecho "Deploying ixia-c operator tests ..." 
    rm -rf operator-tests 2> /dev/null || true
    tar -xvf operator-tests.tar.gz
    docker cp ./kne/kne_cli/kne_cli $KIND_SINGLE_NODE_NAME:/
    docker cp ./operator-tests/kne_configs/enable_ssh_arista_config.txt $KIND_SINGLE_NODE_NAME:/
    python3 -m pip install -r ./operator-tests/py/requirements.txt
}

delete_ixia_configmap() {
    cecho "Deleting ixia-configmap ..."
    kubectl delete -f ixia-configmap.yaml
}

delete_ixia_c_operator() {
    cecho "Deleting ixia-c operator ..."
    kubectl delete -f ixiatg-operator.yaml
    wait_for_pod_counts ixiatg-op-system 0
    wait_for_svc_counts ixiatg-op-system 0
}

delete_ixia_c_test_client() {
    cecho "Deleting ixia-c test client ..."
    kubectl delete -f ixia-c-test-client.yaml
    wait_for_pod_counts default 0
}

delete() {
    delete_ixia_c_test_client
    delete_ixia_configmap
    delete_ixia_c_operator
}

delete_in_kind() {
    docker exec -t $KIND_SINGLE_NODE_NAME /bin/bash -c "./operator-deploy.sh delete"
    docker exec -t $KIND_SINGLE_NODE_NAME /bin/bash -c "rm -rf ./*.sh"
    docker exec -t $KIND_SINGLE_NODE_NAME /bin/bash -c "rm -rf ./*.yaml"
    docker exec -t $KIND_SINGLE_NODE_NAME /bin/bash -c "rm -rf ./*.txt"
}

delete_tests() {
    cecho "Deleting ixia-c operator tests ..." 
    rm -rf operator-tests
    docker exec -t $KIND_SINGLE_NODE_NAME rm -rf ./kne_cli
}


case $1 in
    setup_go  )
        install_go
        ;;
    setup_kne      )
        get_kne
        ;;
    load_images      )
        load_images
        ;;
    deploy      )
        deploy
        ;;
    kind_deploy      )
        deploy_in_kind
        ;;
    deploy_tests )
        deploy_tests
        ;;
    delete      )
        delete
        ;;
    kind_delete      )
        delete_in_kind
        ;;
    delete_images      )
        delete_images
        ;;
    delete_tests    )
        delete_tests
        ;;
	*		)
        $1 || echo "usage: $0 [setup_go|setup_kne|load_images|deploy|kind_deploy|delete|kind_delete|delete_images|delete_tests]"
		;;
esac






