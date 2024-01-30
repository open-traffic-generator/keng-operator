import pytest
import utils
import time

@pytest.mark.sanity
def test_license_server_config():
    """
    1.  Apply configmap with both license server address and image info,
        Add secret for license server address,
        Deploy pdp kne topology with default version,
        Verify:
            License server address in env of Controller
            No license server deployed in Controller POD
    2.  Apply configmap with both license server address and image info,
        Add secret for license server image,
        Deploy pdp kne topology with default version,
        Verify:
            License server address in env of Controller
            No license server deployed in Controller POD
    3.  Apply configmap no license server address or image info,
        Add secret for license server image,
        Deploy pdp kne topology with default version,
        Verify:
            License server address in env of Controller set to localhost
            License server deployed in Controller POD
    4.  Apply configmap with license server image info,
        Deploy pdp kne topology with default version,
        Verify:
            License server address in env of Controller set to localhost
            License server deployed in Controller POD
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'ixia_c_pdp_topology.yaml'
    controller_pod = 'ixia-c'
    expected_pods = [
        'arista1',
        'otg-controller',
        'otg-port-eth1',
        'otg-port-eth2'
    ]
    lic_address = '1.1.1.1'
    lic_path = 'ghcr.io/open-traffic-generator/licensed/keng-license-server'
    lic_tag = 'latest'
    ctrl_pod_name = 'otg-controller'
    ctrl_container_count = 2
    env_name = 'LICENSE_SERVERS'
    try:
        op_rscount = utils.get_operator_restart_count()
        
        # SCENARIO 1: License server address set from secret
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_license_configmap(lic_address, lic_path, lic_tag)
        utils.remove_license_secrets()
        utils.load_license_secrets(lic_address, "")
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.containers_count_ok(ctrl_container_count, ctrl_pod_name, namespace1)
        utils.check_env_data(controller_pod, expected_pods[1], namespace1, env_name, lic_address)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.reset_configmap()
        utils.remove_license_secrets()

        # SCENARIO 2: License server address set from configmap
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_license_configmap(lic_address, lic_path, lic_tag)
        utils.remove_license_secrets()
        utils.load_license_secrets("", lic_path+":"+lic_tag)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.containers_count_ok(ctrl_container_count, ctrl_pod_name, namespace1)
        utils.check_env_data(controller_pod, expected_pods[1], namespace1, env_name, lic_address)

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.reset_configmap()

        # SCENARIO 3: License server image set from secret
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_license_configmap("", lic_path, lic_tag)
        utils.remove_license_secrets()
        utils.load_license_secrets("", lic_path+":"+lic_tag)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        ctrl_container_count = 3
        utils.containers_count_ok(ctrl_container_count, ctrl_pod_name, namespace1)
        utils.check_env_data(controller_pod, expected_pods[1], namespace1, env_name, 'localhost')

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.reset_configmap()
        utils.remove_license_secrets()

        # SCENARIO 4: License server image set from configmap
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.load_license_configmap("", lic_path, lic_tag)
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.containers_count_ok(ctrl_container_count, ctrl_pod_name, namespace1)
        utils.check_env_data(controller_pod, expected_pods[1], namespace1, env_name, 'localhost')

        print("[Namespace:{}]Deleting KNE topology".format(
            namespace1
        ))
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.reset_configmap()

        utils.ixia_c_operator_ok(op_rscount)

    finally:
        utils.delete_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, [])
        utils.reset_configmap()
        utils.remove_license_secrets()

        utils.wait_for(
            lambda: utils.topology_deleted(namespace1),
            'topology deleted',
            timeout_seconds=30
        )
        time.sleep(5)
