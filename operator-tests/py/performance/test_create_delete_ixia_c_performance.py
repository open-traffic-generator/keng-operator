import pytest
import utils
import time


@pytest.mark.sanity
def test_create_delete_ixia_c_performance():
    """
    Deploy one arm kne topology,
    - namespace - 1: ixia-c
    Delete one arm kne topology,
    - namespace - 1: ixia-c
    Validate,
    - pods creation performance
    - pods termination performance
    - total pods count
    - overall pods status
    - total service count
    - individual pod status
    - individual service status
    - operator pod health
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'b2b_ixia_c_namespace.txt'
    expected_svcs = [
        'service-http-otg-controller',
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

    ixia_c_pod_exp_creation_time = 25
    ixia_c_pod_exp_termination_time = 50
    tolerance_percentage = 5
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.create_kne_config(namespace1_config, namespace1)

        creation_time = utils.time_taken_for_pods_to_be_ready(
            namespace1,
            expected_pods
        )
        print("[Namespace:{}] {} pods are running within {} seconds".format(
            namespace1,
            expected_pods,
            creation_time
        ))

        assert utils.time_ok(
            creation_time,
            ixia_c_pod_exp_creation_time,
            tolerance_percentage
        ), "Pods to be running: expected = {} sec, got = {} sec".format(
            ixia_c_pod_exp_creation_time,
            creation_time
        )

        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.ixia_c_services_ok(namespace1, expected_svcs)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)

        termination_time = utils.time_taken_for_pods_to_be_terminated(
            namespace1,
            []
        )
        print("[Namespace:{}] All pods are terminaed within {} seconds".format(
            namespace1,
            termination_time
        ))

        assert utils.time_ok(
            termination_time,
            ixia_c_pod_exp_termination_time,
            tolerance_percentage
        ), "Pods to be terminated: expected = {} sec, got = {} sec".format(
            ixia_c_pod_exp_termination_time,
            termination_time
        )

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
        time.sleep(5)
