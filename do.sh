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

IXIA_C_CONTROLLER=0.0.1-2342
IXIA_C_PROTOCOL_ENGINE=1.00.0.83
IXIA_C_TRAFFIC_ENGINE=1.4.0.15
IXIA_C_GRPC_SERVER=0.6.6
IXIA_C_GNMI_SERVER=0.6.6
ARISTA_CEOS_VERSION=4.26.1F
IXIA_C_TEST_CLIENT=0.0.1-920

GCP_DOCKER_REPO=us-central1-docker.pkg.dev/kt-nts-athena-dev/keysight

LOCK_STATUS_FILE=sanity_lock_status.txt
LOCK_FILE=sanity_lock

SANITY_REPORTS=./reports
SANITY_LOGS=./logs
SANITY_STATUS=FAIL

START_TIME="$(date -u +%s)"
TOTAL_TIME=3600
APPROX_SANITY_TIME=1200

TESTBED_CICD_DIR=operator_cicd

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
    && apt-get -y install curl git openssh-server vim unzip tar make bash wget sshpass \
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
    docker save ${IXIA_C_OPERATOR_IMAGE}:${version} | gzip > ${art}/ixia-c-operator.tar.gz
}

cicd_gen_local_ixia_c_artifacts() {
    ixia_c_art=./ixia_c_art
    mkdir -p ${ixia_c_art}

    echo "Downloading ixia-c-controller:${IXIA_C_CONTROLLER}"
    docker pull ${ARTIFACTORY_DOCKER_REPO}/controller:${IXIA_C_CONTROLLER} \
    && docker tag ${ARTIFACTORY_DOCKER_REPO}/controller:${IXIA_C_CONTROLLER} ${GCP_DOCKER_REPO}/ixia-c-controller:${IXIA_C_CONTROLLER} \
    && docker save ${GCP_DOCKER_REPO}/ixia-c-controller:${IXIA_C_CONTROLLER} | gzip > ${ixia_c_art}/ixia-c-controller.tar.gz

    echo "Downloading ixia-c-test-client:${IXIA_C_TEST_CLIENT}"
    docker pull ${ARTIFACTORY_DOCKER_REPO}/ixia-c-test-client:${IXIA_C_TEST_CLIENT} \
    && docker tag ${ARTIFACTORY_DOCKER_REPO}/ixia-c-test-client:${IXIA_C_TEST_CLIENT} ${GCP_DOCKER_REPO}/ixia-c-test-client:${IXIA_C_TEST_CLIENT} \
    && docker save ${GCP_DOCKER_REPO}/ixia-c-test-client:${IXIA_C_TEST_CLIENT} | gzip > ${ixia_c_art}/ixia-c-test-client.tar.gz

    echo "Downloading ixia-c-traffic-engine:${IXIA_C_TRAFFIC_ENGINE}"
    docker pull docker-local-ixvm-lbj.artifactorylbj.it.keysight.com/athena-traffic-engine:${IXIA_C_TRAFFIC_ENGINE} \
    && docker tag docker-local-ixvm-lbj.artifactorylbj.it.keysight.com/athena-traffic-engine:${IXIA_C_TRAFFIC_ENGINE} ${GCP_DOCKER_REPO}/ixia-c-traffic-engine:${IXIA_C_TRAFFIC_ENGINE} \
    && docker save ${GCP_DOCKER_REPO}/ixia-c-traffic-engine:${IXIA_C_TRAFFIC_ENGINE} | gzip > ${ixia_c_art}/ixia-c-traffic-engine.tar.gz

    echo "Downloading ixia-c-protocol-engine:${IXIA_C_PROTOCOL_ENGINE}"
    docker pull docker-local-nas.artifactorylbj.it.keysight.com/packages_rustic/l23_protocols:${IXIA_C_PROTOCOL_ENGINE} \
    && docker tag docker-local-nas.artifactorylbj.it.keysight.com/packages_rustic/l23_protocols:${IXIA_C_PROTOCOL_ENGINE} ${GCP_DOCKER_REPO}/ixia-c-protocol-engine:${IXIA_C_PROTOCOL_ENGINE} \
    && docker save ${GCP_DOCKER_REPO}/ixia-c-protocol-engine:${IXIA_C_PROTOCOL_ENGINE} | gzip > ${ixia_c_art}/ixia-c-protocol-engine.tar.gz

    echo "Downloading ixia-c-grpc-server:${IXIA_C_GRPC_SERVER}"
    docker pull otgservices/otg-grpc-server:${IXIA_C_GRPC_SERVER} \
    && docker tag otgservices/otg-grpc-server:${IXIA_C_GRPC_SERVER} ${GCP_DOCKER_REPO}/ixia-c-grpc-server:${IXIA_C_GRPC_SERVER} \
    && docker save ${GCP_DOCKER_REPO}/ixia-c-grpc-server:${IXIA_C_GRPC_SERVER} | gzip > ${ixia_c_art}/ixia-c-grpc-server.tar.gz

    echo "Downloading ixia-c-gnmi-server:${IXIA_C_GNMI_SERVER}"
    docker pull otgservices/otg-gnmi-server:${IXIA_C_GNMI_SERVER} \
    && docker tag otgservices/otg-gnmi-server:${IXIA_C_GNMI_SERVER} ${GCP_DOCKER_REPO}/ixia-c-gnmi-server:${IXIA_C_GNMI_SERVER} \
    && docker save ${GCP_DOCKER_REPO}/ixia-c-gnmi-server:${IXIA_C_GNMI_SERVER} | gzip > ${ixia_c_art}/ixia-c-gnmi-server.tar.gz

    echo "Downloading arista-ceos:${ARISTA_CEOS_VERSION}"
    cd ${ixia_c_art}
    curl -kLO "https://artifactory.it.keysight.com/artifactory/generic-local-athena/external/ceos/${ARISTA_CEOS_VERSION}/cEOS64-lab-${ARISTA_CEOS_VERSION}.tar"
    cd ..

    echo "Files in ./ixia_c_art: $(ls -lht ${ixia_c_art})"
}

cicd_exec_on_testbed() {
    cmd=${1}
    sshpass -p ${TESTBED_PASSWORD} ssh -o StrictHostKeyChecking=no  ${TESTBED_USERNAME}@${TESTBED} "${cmd}"
}

cicd_create_lock_status_file() {
    echo "creating lock status file: ${LOCK_STATUS_FILE}"
    touch ${LOCK_STATUS_FILE}
    echo "lock status file: ${LOCK_STATUS_FILE} created"
}

cicd_create_folder_in_testbed() {
    echo "creating cicd folder in testbed: ${TESTBED_CICD_DIR}"
    cicd_exec_on_testbed "mkdir ${TESTBED_CICD_DIR}"
    echo "creating cicd folder: ${TESTBED_CICD_DIR}"
}

cicd_lock_testbed() {
    echo "Locking testbed for sanity"
    cicd_create_folder_in_testbed
    cicd_create_lock_status_file
    echo "Locked testbed for sanity"
}

cicd_check_for_timeout() {
    echo "Checking for timeout in CICD Pipeline"
    END_TIME="$(date -u +%s)"
    ELAPSED="$(($END_TIME-$START_TIME))"
    echo "Time Elapsed: ${ELAPSED} seconds"
    echo "Timeout Given For Pipeline: ${TOTAL_TIME} seconds"
    REM_TIME="$(($TOTAL_TIME-$ELAPSED))"
    echo "Remaining time for pipeline : ${REM_TIME} seconds"
    echo "Estimated Time required to run sanity ${APPROX_SANITY_TIME} seconds"
    if [[ ${REM_TIME} -lt ${APPROX_SANITY_TIME} ]]
    then
        echo "Pipeline may expire during running tests in testbed, please retry the pipeline sometimes later"
        exit 1
    fi
    echo "Pipeline won't expire during running tests in testbed"

}

cicd_wait_for_testbed_to_unlock() {
    local_lock_status=$(ls ${LOCK_STATUS_FILE} 2> /dev/null)
    if [[ ${local_lock_status} ]]
    then
        echo "Local lock status file forund, so testbed is already locked by this process"
    else
        while :
        do
            status=$(cicd_exec_on_testbed "ls -d ${TESTBED_CICD_DIR} 2> /dev/null")
            if  [[ ${status} ]]
            then
                echo "Testbed is locked due to another sanity is still running....."
                echo "Retry after 1 minute"
                sleep 1m
            else
                echo "Testbed is already unlocked"
                cicd_check_for_timeout
                cicd_lock_testbed
                break
            fi
        done 
    fi  
}

cicd_copy_file_to_testbed() {
    for var in "$@"
    do
        echo "pushing ${var} to testbed"
        sshpass -p ${TESTBED_PASSWORD} scp -o StrictHostKeyChecking=no ${var} ${TESTBED_USERNAME}@${TESTBED}:./${TESTBED_CICD_DIR}/
        echo "${var} pushed to testbed"
    done
}

cicd_push_artifacts_to_testbed() {
    art=${1}
    cicd_copy_file_to_testbed ./art/*
    cicd_copy_file_to_testbed ./ixia_c_art/*
    cicd_copy_file_to_testbed ./tests_art/*
}

cicd_run_sanity_in_testbed() {
    version=${1}
    echo "sanity run in testbed: starting"
    sanity_run_cmd="python3 operator_cicd.py --test -build ${version} -mark sanity -ixia_c_release ${IXIA_C_CONTROLLER}"
    cicd_exec_on_testbed "cd ./${TESTBED_CICD_DIR} && sudo -S <<< ${TESTBED_PASSWORD} ${sanity_run_cmd}"
    echo "sanity run in testbed: done"
}

cicd_copy_results_from_testbed() {
    for var in "$@"
    do
        echo "pulling ${var} from testbed"
        sshpass -p ${TESTBED_PASSWORD} scp -o StrictHostKeyChecking=no -r ${TESTBED_USERNAME}@${TESTBED}:/home/${TESTBED_USERNAME}/${TESTBED_CICD_DIR}/${var} ${SANITY_REPORTS}/
        echo "${var} pulled from testbed"
    done
}

cicd_pull_results_from_testbed() {
    version=${1}
    mkdir -p ${SANITY_REPORTS}
    cicd_copy_results_from_testbed reports-*.html sanity-summary-${version}.csv
}

cicd_cleanup_in_testbed() {
    echo "clean up in testbed: starting"
    cicd_exec_on_testbed "cd ${TESTBED_CICD_DIR} && sudo -S <<< ${TESTBED_PASSWORD} python3 operator_cicd.py --clean"
    cicd_exec_on_testbed "cd ${TESTBED_CICD_DIR} && sudo -S <<< ${TESTBED_PASSWORD} rm operator_cicd.py"
    echo "clean up in testbed: done"
}

cicd_check_sanity_status() {
    version=${1}
    echo "checking santy status"
    cat ${SANITY_REPORTS}/sanity-summary-${version}.csv
    sanity_pass=$(grep All ${SANITY_REPORTS}/sanity-summary-${version}.csv | grep -Eo '[0-9]{1,4}')
    echo "Sanity pass rate : ${sanity_pass}"
    sanity_pass_rate=$(echo "${sanity_pass}" | tr -d $'\r' | bc -l)
    if [[ ${sanity_pass_rate} -ge ${EXPECTED_SANITY_PASS_RATE} ]]
    then
        SANITY_STATUS=PASS
    fi
    echo "Sanity expected pass rate : ${EXPECTED_SANITY_PASS_RATE}"
    if [[ ${SANITY_STATUS} == PASS ]]
    then
        echo "sanity : pass"
    else 
        echo "sanity: fail"
        exit 1
    fi
}

cicd_run_sanity() {
    art=${1}
    version=${2}
    cicd_push_artifacts_to_testbed ${art}
    cicd_run_sanity_in_testbed ${version}
    cicd_pull_results_from_testbed ${version}
    cicd_cleanup_in_testbed

    echo "Reports in ${SANITY_REPORTS}: $(ls -lht ${SANITY_REPORTS})"
    cicd_check_sanity_status ${version}
}

cicd_install_deps() {
    echo "Installing CICD deps"
    apk update \
    && apk add curl git openssh vim unzip tar make bash wget sshpass \
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

cicd_gen_tests_artifacts() {
    echo "Generating Ixia-C-Operator test artifacts ..."
    tests_art=./tests_art
    mkdir -p ${tests_art}
    tar -zcvf ${tests_art}/operator-tests.tar.gz ./operator-tests
    cp ./operator-tests/helper/* ${tests_art}/

    cat ${tests_art}/template-ixia-configmap.yaml | \
        sed "s/IXIA_C_CONTROLLER_VERSION/${IXIA_C_CONTROLLER}/g" | \
        sed "s/IXIA_C_GNMI_SERVER_VERSION/${IXIA_C_GNMI_SERVER}/g" | \
        sed "s/IXIA_C_GRPC_SERVER_VERSION/${IXIA_C_GRPC_SERVER}/g" | \
        sed "s/IXIA_C_TRAFFIC_ENGINE_VERSION/${IXIA_C_TRAFFIC_ENGINE}/g" | \
        sed "s/IXIA_C_PROTOCOL_ENGINE_VERSION/${IXIA_C_PROTOCOL_ENGINE}/g" | \
        tee ${tests_art}/ixia-configmap.yaml > /dev/null

    cat ${tests_art}/template-ixia-c-test-client.yaml | \
        sed "s/IXIA_C_TEST_CLIENT/${IXIA_C_TEST_CLIENT}/g" | \
        tee ${tests_art}/ixia-c-test-client.yaml > /dev/null

    rm -rf template-*.yaml
    echo "Files in ./tests_art: $(ls -lht ${tests_art})"
}

cicd () {
    art=./art
    mkdir -p ${art}

    install_deps \
    && gen_ixia_c_op_dep_yaml \
    && get_docker_build \
    && gen_operator_artifacts ${art}
    version=$(echo_version)
    echo "Build Version: $version"
    echo "Files in ./art: $(ls -lht ${art})"

    # cicd_gen_local_ixia_c_artifacts \
    # && cicd_gen_tests_artifacts

    # # pipeline wait for testbed to be unlocked for sanity
    # cicd_wait_for_testbed_to_unlock \
    # && cicd_run_sanity ${art} ${version}

    # if [ ${CI_COMMIT_REF_NAME} = "main" ]
    # then 
    #     cicd_publish_to_docker_repo ${version}
    #     cicd_publish_to_generic_repo ${art} ${version}
    # fi
    docker rmi -f ${IXIA_C_OPERATOR_IMAGE}:${version} 2> /dev/null || true
}


remove_cicd_folder_from_testbed(){
    lock_status=$(ls ${LOCK_STATUS_FILE} 2> /dev/null)
    if [[ ${lock_status} ]]
    then
        status=$(cicd_exec_on_testbed "ls -d ${TESTBED_CICD_DIR} 2> /dev/null")
        if  [[ ${status} ]]
        then
            cicd_exec_on_testbed "sudo -S <<< ${TESTBED_PASSWORD} rm -rf ${TESTBED_CICD_DIR}" 
            echo "${TESTBED_CICD_DIR}: deleted from testbed"
        else
            echo "${TESTBED_CICD_DIR}: not found in testbed"
        fi
    fi
}

remove_testbed_lock_status() {
    echo "removing testbed lock status file"
    lock_status=$(ls ${LOCK_STATUS_FILE} 2> /dev/null)
    if [[ ${lock_status} ]]
    then 
        rm ${LOCK_STATUS_FILE}
        echo "testbed lock status file removed"
    fi
}

unlock_testbed(){
    echo "unlocking testbed..."
    remove_cicd_folder_from_testbed
    remove_testbed_lock_status
    echo "testbed unlocked"
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
    unlock  )
        unlock_testbed
        ;;
	*		)
        $1 || echo "usage: $0 [deps|local|docker|yaml|cicd|version]"
		;;
esac