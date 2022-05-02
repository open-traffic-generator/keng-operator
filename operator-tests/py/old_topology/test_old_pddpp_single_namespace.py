import pytest
import utils
import time
from deepdiff import DeepDiff


@pytest.mark.sanity
def test_old_pddpp_single_namespace():
    """
    Deploy old pddpp kne topology,
    - namespace - 1: ixia-c
    Delete old pddpp kne topology,
    - namespace - 1: ixia-c
    Validate,
    - total pods count
    - overall pods status
    - total service count
    - individual pod status
    - individual service status
    - operator pod health
    - socket connection
    - meshnet topologies
    - ixiatgs
    """
    namespace1 = 'ixia-c'
    namespace1_config = 'old_pddpp_ixia_c_namespace.txt'

    expected_svcs = {
        'ixia-c-service': [443],
        'gnmi-service': [50051],
        'grpc-service': [40051],
        'service-ixia-c-port1': [5555, 50071],
        'service-ixia-c-port2': [5555, 50071],
        'service-ixia-c-port3': [5555, 50071],
        'service-arista1': [],
        'service-arista2': []
    }

    expected_pods = [
        'arista1',
        'arista2',
        'ixia-c',
        'ixia-c-port1',
        'ixia-c-port2',
        'ixia-c-port3'
    ]

    expected_topology = [
        {
            "metadata": {
                "name": "arista1",
                "namespace": "ixia-c"
            },
            "spec": {
                "links": [
                    {
                        "local_intf": "eth1",
                        "local_ip": "",
                        "peer_intf": "eth1",
                        "peer_ip": "",
                        "peer_pod": "ixia-c-port1",
                        "uid": 0
                    },
                    {
                        "local_intf": "eth2",
                        "local_ip": "",
                        "peer_intf": "eth1",
                        "peer_ip": "",
                        "peer_pod": "arista2",
                        "uid": 1
                    }
                ]
            }
        },
        {
            "metadata": {
                "name": "arista2",
                "namespace": "ixia-c"
            },
            "spec": {
                "links": [
                    {
                        "local_intf": "eth2",
                        "local_ip": "",
                        "peer_intf": "eth1",
                        "peer_ip": "",
                        "peer_pod": "ixia-c-port2",
                        "uid": 2
                    },
                    {
                        "local_intf": "eth3",
                        "local_ip": "",
                        "peer_intf": "eth1",
                        "peer_ip": "",
                        "peer_pod": "ixia-c-port3",
                        "uid": 3
                    },
                    {
                        "local_intf": "eth1",
                        "local_ip": "",
                        "peer_intf": "eth2",
                        "peer_ip": "",
                        "peer_pod": "arista1",
                        "uid": 1
                    }
                ]
            }
        },
        {
            "metadata": {
                "name": "ixia-c-port1",
                "namespace": "ixia-c"
            },
            "spec": {
                "links": [
                    {
                        "local_intf": "eth1",
                        "local_ip": "",
                        "peer_intf": "eth1",
                        "peer_ip": "",
                        "peer_pod": "arista1",
                        "uid": 0
                    }
                ]
            }
        },
        {
            "metadata": {
                "name": "ixia-c-port2",
                "namespace": "ixia-c"
            },
            "spec": {
                "links": [
                    {
                        "local_intf": "eth1",
                        "local_ip": "",
                        "peer_intf": "eth2",
                        "peer_ip": "",
                        "peer_pod": "arista2",
                        "uid": 2
                    }
                ]
            }
        },
        {
            "metadata": {
                "name": "ixia-c-port3",
                "namespace": "ixia-c"
            },
            "spec": {
                "links": [
                    {
                        "local_intf": "eth1",
                        "local_ip": "",
                        "peer_intf": "eth3",
                        "peer_ip": "",
                        "peer_pod": "arista2",
                        "uid": 3
                    }
                ]
            }
        }
    ]

    expected_ixiatgs = [
        {
            "metadata": {
                "name": "ixia-c-port1",
                "namespace": "ixia-c"
            },
            "spec": {
                "api_endpoint_map": {
                    "cp": {
                        "in": 50071
                    },
                    "dp": {
                        "in": 5555
                    }
                },
                "desired_state": "DEPLOYED",
                "interfaces": [
                    {
                        "name": "eth1"
                    }
                ],
                "release": "local-old"
            },
            "status": {
                "api_endpoint": {
                    "pod_name": "ixia-c-port1-controller",
                    "service_names": [
                        "service-cp-ixia-c-port1-controller",
                        "service-dp-ixia-c-port1-controller"
                    ]
                },
                "interfaces": [
                    {
                        "interface": "eth1",
                        "name": "eth1",
                        "pod_name": "ixia-c-port1"
                    }
                ],
                "state": "DEPLOYED"
            }
        },
        {
            "metadata": {
                "name": "ixia-c-port2",
                "namespace": "ixia-c"
            },
            "spec": {
                "api_endpoint_map": {
                    "cp": {
                        "in": 50071
                    },
                    "dp": {
                        "in": 5555
                    }
                },
                "desired_state": "DEPLOYED",
                "interfaces": [
                    {
                        "name": "eth1"
                    }
                ],
                "release": "local-old"
            },
            "status": {
                "api_endpoint": {
                    "pod_name": "ixia-c-port2-controller",
                    "service_names": [
                        "service-dp-ixia-c-port2-controller",
                        "service-cp-ixia-c-port2-controller"
                    ]
                },
                "interfaces": [
                    {
                        "interface": "eth1",
                        "name": "eth1",
                        "pod_name": "ixia-c-port2"
                    }
                ],
                "state": "DEPLOYED"
            }
        },
        {
            "metadata": {
                "name": "ixia-c-port3",
                "namespace": "ixia-c"
            },
            "spec": {
                "api_endpoint_map": {
                    "cp": {
                        "in": 50071
                    },
                    "dp": {
                        "in": 5555
                    }
                },
                "desired_state": "DEPLOYED",
                "interfaces": [
                    {
                        "name": "eth1"
                    }
                ],
                "release": "local-old"
            },
            "status": {
                "api_endpoint": {
                    "pod_name": "ixia-c-port3-controller",
                    "service_names": [
                        "service-cp-ixia-c-port3-controller",
                        "service-dp-ixia-c-port3-controller"
                    ]
                },
                "interfaces": [
                    {
                        "interface": "eth1",
                        "name": "eth1",
                        "pod_name": "ixia-c-port3"
                    }
                ],
                "state": "DEPLOYED"
            }
        }
    ]

    try:
        op_rscount = utils.get_operator_restart_count()
        print("[Namespace:{}]Deploying KNE topology".format(
            namespace1
        ))
        utils.create_kne_config(namespace1_config, namespace1)
        utils.ixia_c_pods_ok(namespace1, expected_pods)
        utils.ixia_c_services_ok(namespace1, list(expected_svcs.keys()))
        op_rscount = utils.ixia_c_operator_ok(op_rscount)

        actual_topologies = utils.get_topologies(namespace1)
        assert not DeepDiff(expected_topology, actual_topologies, ignore_order=True), "expected topologies mismatched!!!"

        actual_ixiatgs = utils.get_ixiatgs(namespace1)
        assert not DeepDiff(expected_ixiatgs, actual_ixiatgs, ignore_order=True), "expected ixiatgs mismatched!!!"

        svc_ingress_map = utils.get_ingress_mapping(namespace1, list(expected_svcs.keys()))
        utils.socket_alive(expected_svcs, svc_ingress_map)

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
