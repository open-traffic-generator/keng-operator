import pytest
import utils
import time


def test_liveness_default_config():
    """
    Deploy b2b kne topology with default version,
    - namespace - 1: ixia-c
    Delete b2b kne topology,
    - namespace - 1: ixia-c
    Validate,
    - default liveness parameters for protocol engines
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'ixia_c_default_config.txt'
    expected_pods = [
        'otg-controller',
        'otg-port-eth1',
        'otg-port-eth2'
    ]
    container_extensions = [
        '-protocol-engine',
        '-traffic-engine'
    ]
    lic_path = 'docker-local-athena.artifactory.it.keysight.com/keng-license-server'
    lic_tag = '0.0.1-32'
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_license_configmap("", lic_path, lic_tag)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.check_liveness_data('ixia-c', expected_pods[0], namespace1, True, 1, 10, 6)
        utils.check_liveness_data('gnmi', expected_pods[0], namespace1, True, 1, 10, 6)
        utils.check_liveness_data('license-server', expected_pods[0], namespace1, True, 1, 10, 6)
        utils.check_liveness_data(expected_pods[1]+container_extensions[0], expected_pods[1], namespace1, True, 1, 10, 6)
        utils.check_liveness_data(expected_pods[1]+container_extensions[1], expected_pods[1], namespace1, True, 1, 10, 6)
        utils.check_liveness_data(expected_pods[2]+container_extensions[0], expected_pods[2], namespace1, True, 1, 10, 6)
        utils.check_liveness_data(expected_pods[2]+container_extensions[1], expected_pods[2], namespace1, True, 1, 10, 6)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

    finally:
        utils.reset_configmap()
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        time.sleep(5)
