import pytest
import utils
import time

@pytest.mark.sanity
def test_cm_reload_single_namespace():
    """
    Deploy pd kne topology with BAD config,
    - namespace - 1: ixia-c
    Delete pd kne topology,
    - namespace - 1: ixia-c
    Validate,
    - total pods count
    - overall pods status
    - individual pod status
    - operator pod health
    Deploy pd kne topology with GOOD config,
    - namespace - 1: ixia-c
    Delete pd kne topology,
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
    namespace1_config = 'ixia_c_pd_topology.yaml'
    expected_svcs = [
        'service-gnmi-otg-controller',
        'service-grpc-otg-controller',
        'service-otg-port-eth1',
        'service-arista1',
    ]

    expected_good_pods = [
        'otg-controller',
        'arista1'
    ]

    expected_bad_pods_status = 'ImagePullBackOff'

    expected_bad_pods = [
        'otg-port-eth1',
    ]

    expected_pods = expected_good_pods + expected_bad_pods

    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_bad_configmap("protocol-engine")
        utils.create_kne_config(namespace1_config, namespace1, False)
        # Wait for topology to be created
        time.sleep(10)
        utils.ixia_c_pod_status_match(namespace1, expected_bad_pods[0], expected_bad_pods_status)
        utils.ixia_c_pods_ok(namespace1, expected_good_pods, False)
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
        time.sleep(10)
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.reset_configmap()
        time.sleep(2)
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
        utils.reset_configmap()
        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        time.sleep(5)
