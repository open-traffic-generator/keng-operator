import pytest
import utils

@pytest.mark.sanity
def test_init_container():
    """
    Deploy neg-vm kne topology,
    - namespace - 1: ixia-c
    Delete neg-vm kne topology,
    - namespace - 1: ixia-c
    Validate,
    - individual pod status
    - operator pod health
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'b2b_ixia_c_namespace.txt'
    expected_pods = [
        'ixia-c',
        'ixia-c-port1',
        'ixia-c-port2'
    ]
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_init_configmap()
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods, False)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

    finally:
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])
        utils.unload_init_configmap()



