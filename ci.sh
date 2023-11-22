#!/bin/sh

export PATH=$PATH:/usr/local/go/bin/

GO_VERSION=1.17.9

# Avoid warnings for non-interactive apt-get install
export DEBIAN_FRONTEND=noninteractive


IXIA_C_OPERATOR_IMAGE=keng-hybrid-operator
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
    export IMAGE_TAG_BASE=${IXIA_C_OPERATOR_IMAGE}
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
    docker login -p ${TOKEN_GITHUB} -u biplamal ghcr.io
    if DOCKER_CLI_EXPERIMENTAL=enabled docker manifest inspect ${img} >/dev/null; then
        docker logout ghcr.io
        return 0
    else
        docker logout ghcr.io
        return 1
    fi
}

push_github_docker_image() {
    img=${1}
    echo "Pushing image ${img} in GitHub"
    docker login -p ${TOKEN_GITHUB} -u biplamal ghcr.io \
    && docker push "${img}" \
    && docker logout ghcr.io \
    && echo "${img} pushed in GitHub" \
    && docker rmi "${img}" > /dev/null 2>&1 || true
}

verify_github_images() {
    for var in "$@"
    do
        img=${var}
        docker rmi -f $img > /dev/null 2>&1 || true
        echo "pulling ${img} from GitHub"
        docker login -p ${TOKEN_GITHUB} -u biplamal ghcr.io
        docker pull $img
        docker logout ghcr.io
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
    version=$(get_version)
    img="${IXIA_C_OPERATOR_IMAGE}:${version}"
    github_img="${GITHUB_REPO}/${IXIA_C_OPERATOR_IMAGE}:${version}"
    github_img_latest="${GITHUB_REPO}/${IXIA_C_OPERATOR_IMAGE}:latest"
    docker tag ${img} "${github_img}"
    docker tag "${github_img}" "${github_img_latest}"
    if github_docker_image_exists ${github_img}; then
        echo "${github_img} already exists..."
	exit 1
    else
        echo "${github_img} does not exist..."
        push_github_docker_image ${github_img}
        verify_github_images ${github_img}

        push_github_docker_image ${github_img_latest}
        verify_github_images ${github_img_latest}
    fi

    cicd_gen_release_art
}

cicd_gen_release_art() {
    mkdir -p ${release}
    rm -rf ./ixiatg-operator.yaml
    rm -rf ${release}/*.yaml
    gen_ixia_c_op_dep_yaml "${GITHUB_REPO}/${IXIA_C_OPERATOR_IMAGE}"
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
    docker save ${IXIA_C_OPERATOR_IMAGE}:${version} | gzip > ${art}/keng-operator.tar.gz
}

build() {
    mkdir -p ${art}
    gen_ixia_c_op_dep_yaml ${IXIA_C_OPERATOR_IMAGE} \
    && get_docker_build \
    && gen_operator_artifacts ${art}
    version=$(get_version)
    echo "Build Version: $version"
    echo "Files in ./art: $(ls -lht ${art})"
}



case $1 in
    deps   )
        install_deps
        ;;
    local   )
        get_local_build
        ;;
    docker   )
        get_docker_build
        ;;
    yaml   )
        gen_ixia_c_op_dep_yaml ${IXIA_C_OPERATOR_IMAGE}
        ;;
    build   )
        build
        ;;
    publish    )
        publish
        ;;
    version   )
        echo_version
        ;;
	*		)
        $1 || echo "usage: $0 [deps|local|docker|yaml|version]"
		;;
esac
