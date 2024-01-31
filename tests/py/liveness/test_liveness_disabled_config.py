import pytest
import utils
import time

@pytest.mark.liveness
def test_liveness_disabled_config():
    """
    Deploy pd kne topology with default version,
    - namespace - 1: ixia-c
    Delete pd kne topology,
    - namespace - 1: ixia-c
    Validate,
    - disabled liveness parameters for protocol engines
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'ixia_c_pd_topology.yaml'
    expected_pods = [
        'otg-controller',
        'otg-port-eth1',
        'arista1'
    ]
    container_extension = '-traffic-engine'
    probe_params = {'traffic-engine':{'liveness-enable': False}}
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_liveness_configmap(probe_params)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.check_liveness_data(expected_pods[1]+container_extension, expected_pods[1], namespace1, False)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)
        utils.reset_configmap()

    finally:
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.reset_configmap()

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        time.sleep(5)