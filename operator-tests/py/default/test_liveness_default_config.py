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
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.check_liveness_data(expected_pods[1], namespace1, True, 10, 1, 3)
        utils.check_liveness_data(expected_pods[2], namespace1, True, 10, 1, 3)
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

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        time.sleep(5)