import utils
import pytest


@pytest.mark.sanity
def test_rest_single_namespace(ixia_c_release):
    """
    Config Map will be fetched via REST call from Ixia-C Release
    Deploy dut kne topology,
    - namespace - 1: ixia-c-rest
    Delete dut kne topology,
    - namespace - 1: ixia-c-rest
    Validate,
    - total pods count
    - overall pods status
    - total service count
    - individual pod status
    - individual service status
    - operator pod health
    """
    namespace1 = 'ixia-c-rest'
    namespace1_config = 'rest_ixia_c_namespace.txt'
    expected_svcs = [
        'service-arista1',
        'service-arista2',
        'service-https-otg-controller',
        'service-gnmi-otg-controller',
        'service-grpc-otg-controller',
        'service-otg-port-eth1',
        'service-otg-port-eth2',
        'service-otg-port-eth3'
    ]

    expected_pods = [
        'arista1',
        'arista2',
        'otg-controller',
        'otg-port-eth1',
        'otg-port-eth2',
        'otg-port-eth3'
    ]

    utils.generate_rest_config_from_temaplate(
        namespace1_config,
        ixia_c_release
    )
    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.ixia_c_services_ok(namespace1, expected_svcs)
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        arista_pods = [
            'arista1',
            'arista2'
        ]
        utils.arista_sshable_ok(arista_pods, namespace1)

        print("[Namespace:{}]Checking E2E tests "
              "running fine or not ...".format(
                  namespace1
              ))
        # utils.ixia_c_e2e_test_ok(
        #     namespace1,
        #     'TestEbgpv4Routes',
        #     'sanity'
        # )

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
        utils.delete_config(namespace1_config)

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        utils.delete_namespace(namespace1)
