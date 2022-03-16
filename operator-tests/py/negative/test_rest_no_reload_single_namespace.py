import pytest
import utils
import time


def test_rest_no_reload_single_namespace():
    """
    Deploy b2b kne topology with latest version,
    - namespace - 1: ixia-c
    Delete b2b kne topology,
    - namespace - 1: ixia-c
    Validate,
    - total pods count
    - overall pods status
    - total service count
    - individual pod status
    - individual service status
    - operator pod health
    Deploy b2b kne topology with BAD config and latest release,
    - namespace - 1: ixia-c
    Delete b2b kne topology,
    - namespace - 1: ixia-c
    Validate,
    - total pods count
    - overall pods status
    - total service count
    - individual pod status
    - individual service status
    - operator pod health
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'ixia_c_default_config.txt'
    expected_svcs = [
        'service-gnmi-otg-controller',
        'service-grpc-otg-controller',
        'service-otg-port-eth1',
        'service-otg-port-eth2'
    ]

    expected_pods = [
        'otg-controller',
        'otg-port-eth1',
        'otg-port-eth2'
    ]
    expected_pods_bad = [
        'otg-controller'
    ]
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.ixia_c_services_ok(namespace1, expected_svcs)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.ixia_c_services_ok(namespace1, [])
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        # Wait for topology to be deleted
        time.sleep(20)
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_bad_configmap("protocol-engine", True)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods_bad, False)
        utils.ixia_c_services_ok(namespace1, expected_svcs)
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
        utils.unload_bad_configmap()
