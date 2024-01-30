import pytest
import utils

@pytest.mark.sanity
def test_args_cmd_env_override():
    """
    Deploy b2b kne topology with default version, but custom args, cmd and env
    - namespace - 1: ixia-c
    Delete b2b kne topology,
    - namespace - 1: ixia-c
    Validate,
    - total pods count
    - total service count
    - individual pod description
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'ixia_c_pd_topology.yaml'
    expected_svcs = [
        'service-gnmi-otg-controller',
        'service-grpc-otg-controller',
        'service-otg-port-eth1',
        'service-arista1',
    ]

    expected_pods = [
        'otg-controller',
        'otg-port-eth1',
        'arista1'
    ]
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_custom_configmap()
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods, True, False, True)
        utils.ixia_c_custom_pods_ok(namespace1)
        utils.ixia_c_services_ok(namespace1, expected_svcs)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)
        utils.reset_configmap()
    finally:
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])
        utils.reset_configmap()



