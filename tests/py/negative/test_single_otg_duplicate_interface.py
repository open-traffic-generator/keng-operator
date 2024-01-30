import utils
import pytest
import time
from deepdiff import DeepDiff

@pytest.mark.sanity
def test_single_interface_connected_multiple_interfaces():
    """
    Deploy single otg duplicate interfaces kne topology,
    - namespace - 1: ixia-c
    Validate,
    - kne_cli error
    - total pods count - 0
    - total service count - 0
    - operator pod health
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'single_otg_duplicate_interface.yaml'
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        _, err = utils.create_kne_config(namespace1_config, namespace1)
        expected_err = "could not find peer for node otg pod otg-port-eth1 link UID 0"
        err = err.split("\n")[-4]
        assert expected_err in err, "Expected error mismatch!!!"
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

    finally:
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        utils.delete_namespace(namespace1)