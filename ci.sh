#!/bin/sh

export PATH=$PATH:/usr/local/go/bin/

GO_VERSION=1.23.0

# Avoid warnings for non-interactive apt-get install
export DEBIAN_FRONTEND=noninteractive


KENG_OPERATOR_IMAGE=keng-operator
GO_TARGZ=""

GITHUB_REPO="ghcr.io/open-traffic-generator"

art=./art
release=./release

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


install_deps() {
	# Dependencies required by this project
    echo "Installing Dependencies ..."
    sudo apt-get update \
	&& sudo apt-get -y install --no-install-recommends apt-utils dialog 2>&1 \
    && sudo apt-get -y install curl make build-essential \
    && get_go \
    && get_go_deps
}

get_go() {
    echo "Installing Go ..."
    # install golang per https://golang.org/doc/install#tarball
    curl -kL https://dl.google.com/go/${GO_TARGZ} | tar -C /usr/local/ -xzf -
    go version
}

get_go_deps() {
    # download all dependencies mentioned in go.mod
    echo "Dowloading go mod dependencies ..."
    go mod download
}

get_version() {
    version=$(head ./version | cut -d' ' -f1)
    echo ${version}
}

echo_version() {
    version=$(head ./version | cut -d' ' -f1)
    echo "gRPC version : ${version}"
}

get_local_build() {
    # Generating local build using Makefile
    echo "Generating local build ..."
    make build
}

get_docker_build() {
    # Generating docker build using Makefile
    echo "Generating docker build ..."
    export VERSION=$(get_version)
    export IMAGE_TAG_BASE=${KENG_OPERATOR_IMAGE}
    make docker-build
    docker rmi -f $(docker images | grep '<none>') 2> /dev/null || true
}

gen_ixia_c_op_dep_yaml() {
    # Generating keng-operator deployment yaml using Makefile
    img=${1}
    echo "Generating keng-operator deployment yaml ..."
    export VERSION=$(get_version)
    export IMAGE_TAG_BASE=${img}
    make yaml
}

github_docker_image_exists() {
    img=${1}
    login_ghcr
    if DOCKER_CLI_EXPERIMENTAL=enabled docker manifest inspect ${img} >/dev/null; then
        logout_ghcr
        return 0
    else
        logout_ghcr
        return 1
    fi
}

login_ghcr() {
    echo "Logging into docker repo ghcr.io"
    echo "${GITHUB_PAT}" | docker login -u"${GITHUB_USER}" --password-stdin ghcr.io
}

logout_ghcr() {
    docker logout ghcr.io
}

push_github_docker_image() {
    img=${1}
    echo "Pushing image ${img} in GitHub"
    login_ghcr \
    && docker push "${img}" \
    && logout_ghcr \
    && echo "${img} pushed in GitHub" \
    && docker rmi "${img}" > /dev/null 2>&1 || true
}

verify_github_images() {
    for var in "$@"
    do
        img=${var}
        docker rmi -f $img > /dev/null 2>&1 || true
        echo "pulling ${img} from GitHub"
        login_ghcr
        docker pull $img
        logout_ghcr
        if docker image inspect ${img} >/dev/null 2>&1; then
            echo "${img} pulled successfully from GitHub"
            docker rmi $img > /dev/null 2>&1 || true
        else
            echo "${img} not found locally!!!"
            docker rmi $img > /dev/null 2>&1 || true
            exit 1
        fi
    done
}

publish() {
    branch=${1}
    docker images
    version=$(get_version)
    docker load -i release/keng-operator.tar.gz
    github_img="${GITHUB_REPO}/${KENG_OPERATOR_IMAGE}:${version}"
    github_img_latest="${GITHUB_REPO}/${KENG_OPERATOR_IMAGE}:latest"
    docker tag ${img} "${github_img}"
    if [ "$branch" = "main" ]
    then
        if github_docker_image_exists ${github_img}; then
            echo "${github_img} already exists..."
            exit 1
        fi
    fi
        
    echo "${github_img} does not exist..."
    push_github_docker_image ${github_img}
    verify_github_images ${github_img}
    if [ "$branch" = "main" ]
    then 
        docker tag "${github_img}" "${github_img_latest}"
        push_github_docker_image ${github_img_latest}
        verify_github_images ${github_img_latest}
    fi
}

gen_artifacts() {
    version=$(get_version)
    mkdir -p ${release}

    rm -rf ./keng-operator.tar.gz
    rm -rf ${release}/*.tar.gz
    img="${KENG_OPERATOR_IMAGE}:${version}"
    github_img="${GITHUB_REPO}/${KENG_OPERATOR_IMAGE}:${version}"
    docker tag $img $github_img
    docker save ${github_img} | gzip > ${release}/keng-operator.tar.gz

    
    gen_ixia_c_op_dep_yaml "${GITHUB_REPO}/${KENG_OPERATOR_IMAGE}"
    mv ./ixiatg-operator.yaml ${release}/
    echo "Files in ./release: $(ls -lht ${release})"
}

gen_operator_artifacts() {
    echo "Generating keng-operator offline artifacts ..."
    art=${1}
    version=$(get_version)
    rm -rf ${art}/*.yaml
    rm -rf ${art}/*.tar.gz
    mv ./ixiatg-operator.yaml ${art}/
    docker save ${KENG_OPERATOR_IMAGE}:${version} | gzip > ${art}/keng-operator.tar.gz
}

build() {
    mkdir -p ${art}
    gen_ixia_c_op_dep_yaml ${KENG_OPERATOR_IMAGE} \
    && get_docker_build \
    && gen_operator_artifacts ${art}
    version=$(get_version)
    echo "Build Version: $version"
    echo "Files in ./art: $(ls -lht ${art})"
}


create_setup() {
    cd tests 
    ./setup.sh rm_k8s_cluster 2> /dev/null || true
    ./setup.sh new_k8s_cluster kne arista
    cd ..
}

setup_pre_test() {
    cd tests 
    ./setup.sh pre_test
    cd ..
}

run_test() {
    cd tests 
    ./setup.sh test ${1}
    grep deselected  logs-${1}/pytest.log | grep ===== | grep failed && return 1 || true
    cd ..
}

destroy_setup() {
    cd tests 
    ./setup.sh rm_k8s_cluster 2> /dev/null || true
    cd ..
}

usage() {
    echo "usage: $0 [name of any function in script]"
    exit 1
}



case $1 in
    *   )
        # shift positional arguments so that arg 2 becomes arg 1, etc.
        cmd=${1}
        shift 1
        ${cmd} ${@} || usage
    ;;
esac
