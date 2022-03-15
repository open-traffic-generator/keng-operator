import pytest
import utils


def test_dut_single_namespace():
    """
    Deploy dut kne topology,
    - namespace - 1: ixia-c
    Delete dut kne topology,
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
    namespace1_config = 'dut_ixia_c_namespace.txt'
    expected_svcs = [
        'service-http-otg-controller',
        'service-gnmi-otg-controller',
        'service-grpc-otg-controller',
        'service-arista1',
        'service-otg-port-eth1',
        'service-otg-port-eth2'
    ]

    expected_pods = [
        'arista1',
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



