import pytest
import utils
import time

@pytest.mark.liveness
def test_startup_default_config():
    """
    Deploy pd kne topology with default version,
    - namespace - 1: ixia-c
    Delete pd kne topology,
    - namespace - 1: ixia-c
    Validate,
    - default startup probe parameters for all ixia-c components
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'ixia_c_pd_topology.yaml'
    expected_pods = [
        'otg-controller',
        'otg-port-eth1',
        'arista1'
    ]
    container_extensions = [
        '-protocol-engine',
        '-traffic-engine'
    ]
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.check_probe_data('ixia-c', expected_pods[0], namespace1, False, True, 0, 3, 20)
        utils.check_probe_data('gnmi', expected_pods[0], namespace1, False, True, 0, 3, 20)
        utils.check_probe_data('license-server', expected_pods[0], namespace1, False, True, 0, 3, 20)
        utils.check_probe_data(expected_pods[1]+container_extensions[0], expected_pods[1], namespace1, False, True, 0, 3, 20)
        utils.check_probe_data(expected_pods[1]+container_extensions[1], expected_pods[1], namespace1, False, True, 0, 3, 20)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

    finally:
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        time.sleep(5)