#!/bin/sh

export PATH=$PATH:/usr/local/go/bin/

GO_VERSION=1.16.5

# Avoid warnings for non-interactive apt-get install
export DEBIAN_FRONTEND=noninteractive

# these will be filled by get_version()
BUILD_VERSION=""
BUILD_REVISION=""
BUILD_COMMIT_HASH=""

IXIA_C_OPERATOR_IMAGE=ixia-c-operator
GO_TARGZ=""

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
    apt-get update \
	&& apt-get -y install --no-install-recommends apt-utils dialog 2>&1 \
    && apt-get -y install curl git ssh vim unzip tar \
    && get_go \
    && get_go_deps
}

get_go() {
    echo "Installing Go ..."
    # install golang per https://golang.org/doc/install#tarball
    curl -kL https://dl.google.com/go/${GO_TARGZ} | tar -C /usr/local/ -xzf -
}

get_go_deps() {
    # download all dependencies mentioned in go.mod
    echo "Dowloading go mod dependencies ..."
    go mod download
}

get_new_version() {
    # output of git describe
    # tag1-1-gb772e8b
    # ^    ^  ^
    # |    |  |
    # |    |  git hash of the commit
    # |    |
    # |   number of commits after the tag
    # |
    # |
    # Most recent tag
    version=$(git describe --tags 2> /dev/null || echo v0.0.1-1-$(git rev-parse --short HEAD))
    BUILD_VERSION=$(echo $version | cut -d- -f1 | cut -dv -f2)
    BUILD_REVISION=$(echo $version | cut -d- -f2)
    BUILD_COMMIT_HASH=$(echo $version | cut -d- -f3 | sed -e "s/^g//g")
}

echo_version() {
    get_new_version
    echo "${BUILD_VERSION}-${BUILD_REVISION}"
}

get_local_build() {
    # Generating local build using Makefile
    echo "Generating local build ..."
    make build
}

get_docker_build() {
    # Generating docker build using Makefile
    echo "Generating docker build ..."
    export VERSION=$(echo_version)
    export IMAGE_TAG_BASE=${IXIA_C_OPERATOR_IMAGE}
    make docker-build
    docker rmi -f $(docker images | grep '<none>') 2> /dev/null || true
}

gen_ixia_c_op_dep_yaml() {
    # Generating ixia-c-operator deployment yaml using Makefile
    echo "Generating ixia-c-operator deployment yaml ..."
    export VERSION=$(echo_version)
    export IMAGE_TAG_BASE=${IXIA_C_OPERATOR_IMAGE}
    make yaml
}

cicd_is_dev_branch() {
    echo "Current ref: ${CI_COMMIT_REF_NAME}"
    echo "${CI_COMMIT_REF_NAME}" | grep --color=no -E "^dev-"
}

cicd_publishing_docker_images() {
    for var in "$@"
    do
        image=${var}
        echo "Publishing ${image}..."
        docker login -p ${ARTIFACTORY_KEY} -u ${ARTIFACTORY_USER} ${ARTIFACTORY_DOCKER_REPO} \
        && docker push ${image} \
        && docker logout ${ARTIFACTORY_DOCKER_REPO}
        echo "${image} Published"
    done
}

cicd_publish_to_docker_repo() {
    version=${1}
    # don't post images when on dev-* branch
    cicd_is_dev_branch && return 0
    echo "Publishing ixia-c-operator images to artifactory docker repo ..."
    op="${ARTIFACTORY_DOCKER_REPO}/${IXIA_C_OPERATOR_IMAGE}:${version}"
    op_latest="${ARTIFACTORY_DOCKER_REPO}/${IXIA_C_OPERATOR_IMAGE}:latest"
    docker tag ${IXIA_C_OPERATOR_IMAGE}:${version} ${op} \
    && docker tag ${op} ${op_latest} \
    && cicd_publishing_docker_images ${op} ${op_latest}

    docker rmi -f $op $op_latest 2> /dev/null || true
    echo "Created docker images has been deleted from runner"
    cicd_verify_dockerhub_images ${op} ${op_latest}
}


cicd_verify_dockerhub_images() {
    echo "Verfying posted images in ${ARTIFACTORY_DOCKER_REPO}"
    for var in "$@"
    do
        image=${var}
        echo "pulling ${image} from ${ARTIFACTORY_DOCKER_REPO}"
        docker pull ${image}
        if docker image inspect ${image} >/dev/null 2>&1; then
            echo "${image} pulled successfully from ${ARTIFACTORY_DOCKER_REPO}"
            docker rmi -f $image 2> /dev/null 2> /dev/null || true
        else
            echo "${image} not found locally!!!"
            docker rmi -f $image 2> /dev/null 2> /dev/null || true
            exit 1
        fi
    done
}

cicd_publish_to_generic_repo() {
    art=${1}
    version=${2}
    targetfolder="external"

    # don't post images when on dev-* branch
    cicd_is_dev_branch && return 0;

    echo "Publishing ixia-c-operator artifacts to generic repo ..."
    target="https://artifactory.it.keysight.com/artifactory/generic-local-athena/${targetfolder}/operator/${version}"
   
    for filename in ${art}/*
    do
        # return immediately curl fails
        curl -H "X-JFrog-Art-Api:${ARTIFACTORY_KEY}" -T ${filename} "${target}/$(basename ${filename})" || return 1
    done
}

gen_operator_artifacts() {
    echo "Generating ixia-c-operator offline artifacts ..."
    art=${1}
    version=$(echo_version)
    rm -rf ${art}/*.yaml
    rm -rf ${art}/*.tar.gz
    mv ./ixiatg-operator.yaml ${art}/
    docker save ${IXIA_C_OPERATOR_IMAGE}:${version} | gzip > ${art}/ixia-c-oprator.tar.gz
}

cicd_install_deps() {
    echo "Installing CICD deps"
    apk update \
    && apk add curl git openssh vim unzip tar make bash wget \
    && apk add --no-cache libc6-compat \
    && apk add build-base

    echo "Installing go in alpine ..."
    wget https://dl.google.com/go/${GO_TARGZ} \
    && tar -C /usr/local -xzf ${GO_TARGZ}
    export PATH=$PATH:/usr/local/go/bin
    go version

    echo "Installing go mod dependencies in alpine ..."
    go mod download
}

cicd () {
    art=./art
    mkdir -p ${art}

    cicd_install_deps \
    && gen_ixia_c_op_dep_yaml \
    && get_docker_build \
    && gen_operator_artifacts ${art}

    version=$(echo_version)
    echo "Build Version: $version"
    echo "Files in ./art: $(ls -lht ${art})"

    if [ ${CI_COMMIT_REF_NAME} = "main" ]
    then 
        cicd_publish_to_docker_repo ${version}
        cicd_publish_to_generic_repo ${art} ${version}
    fi
    docker rmi -f ${IXIA_C_OPERATOR_IMAGE}:${version} 2> /dev/null || true
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
        gen_ixia_c_op_dep_yaml
        ;;
    cicd   )
        cicd
        ;;
    version   )
        echo_version
        ;;
	*		)
        $1 || echo "usage: $0 [deps|local|docker|yaml|cicd|version]"
		;;
esac