import pytest
import utils
import time

@pytest.mark.sanity
def test_min_resource_custom_config():
    """
    Deploy b2b kne topology with default version,
    - namespace - 1: ixia-c
    Delete b2b kne topology,
    - namespace - 1: ixia-c
    Validate,
    - default minimum resource for all components
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
        custom_resource_params = {'protocol-engine':{'cpu': '300m','memory': '50Mi'}, 'traffic-engine':{'cpu': '50m', 'memory': '170Mi'}, 'controller':{'cpu': '50m', 'memory': '190Mi'}, 'gnmi-server':{'cpu': '70m', 'memory': '90Mi'}}
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_min_resource_configmap(custom_resource_params)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.check_min_resource_data('ixia-c', expected_pods[0], namespace1, '190Mi', '50m')
        utils.check_min_resource_data('gnmi', expected_pods[0], namespace1, '90Mi', '70m')
        utils.check_min_resource_data(expected_pods[1]+container_extensions[0], expected_pods[1], namespace1, '50Mi', '300m')
        utils.check_min_resource_data(expected_pods[1]+container_extensions[1], expected_pods[1], namespace1, '170Mi', '50m')
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
