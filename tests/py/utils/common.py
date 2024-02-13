import os
import subprocess
import time
import json
import yaml
import socket

SUDO_USER = 'root'

# path to dir containing kne configurations relative root dir
CONFIGS_DIR = 'topology'
BAD_CONFIG_MAP_FILE = 'bad-ixia-c-config.yaml'
INIT_CONFIG_MAP_FILE = 'init-config.yaml'
BASIC_IXIA_CONFIG_MAP_FILE = 'deployments/basic-ixia-c-config.yaml'
IXIA_CONFIG_MAP_FILE = 'deployments/ixia-c-config.yaml'
CUSTOM_CONFIG_MAP_FILE = 'custom-ixia-c-config.yaml'

KIND_SINGLE_NODE_NAME = 'kind-control-plane'


def exec_shell(cmd, sudo=True, check_return_code=True, wait=True):
    """
    Executes a command in native shell and returns output as str on success or,
    None on error.
    """
    if not sudo:
        cmd = 'sudo -u ' + SUDO_USER + ' ' + cmd

    print('Executing `%s` ...' % cmd)
    if wait == True:
        p = subprocess.Popen(
            cmd.encode('utf-8', errors='ignore'),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, shell=True
        )
        out, err = p.communicate()
        out = out.decode('utf-8', errors='ignore')
        err = err.decode('utf-8', errors='ignore')

        print('Output:\n%s' % out)
        print('Error:\n%s' % err)

        if check_return_code:
            if p.returncode == 0:
                return out, err
            return None, err
        else:
            return out, err
    else:
        subprocess.Popen(cmd.split(" "))
        return "", None


def get_kne_config_path(config_name):
    sep = os.path.sep
    return sep.join([
        CONFIGS_DIR,
        config_name
    ])


def topology_deleted(namespace):
    cmd = "kubectl get topology -n {}".format(
        namespace
    )
    _, err = exec_shell(cmd, True, False)
    err = err.split('\n')

    if 'No resources found' in err[0]:
        return True
    return False


def create_kne_config(config_name, namespace, wait=True):
    config_path = get_kne_config_path(config_name)
    # Ensure topology was deleted from past run
    wait_for(
        lambda: topology_deleted(namespace),
        'ensured topology does not exists',
        timeout_seconds=120
    )
    cmd = "kne create {}".format(
        config_path
    )
    out, err = exec_shell(cmd, True, False, wait)
    return out, err
    


def delete_kne_config(config_name, namespace):
    config_path = get_kne_config_path(config_name)
    cmd = "kne delete ./{}".format(
        config_path
    )
    exec_shell(cmd, True, False)
    wait_for(
        lambda: topology_deleted(namespace),
        'topology deleted',
        timeout_seconds=30
    )


def apply_configmap(configmap):
    cmd = "kubectl apply -f {}".format(
        configmap
    )
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        raise Exception("Failed to apply configmap {} "
                        "inside kind container".format(
                            configmap
                        ))
    else:
        print("configmap {} applied inside kind container".format(
            configmap
        ))


def create_secret(namespace, data):
    cmd = "kubectl create secret -n {} {}".format(namespace, data)
    print(cmd)
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        raise Exception("Failed to create secret {} "
                        "inside kind container".format(
                            data
                        ))
    else:
        print("secret {} created inside kind container".format(
            data
        ))


def load_custom_configmap():
    print("Loading custom container config...")
    cmd = "cat {}".format(
        IXIA_CONFIG_MAP_FILE
    )
    out, _ = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    for elem in json_obj["images"]:
        if elem["name"] == "controller":
            elem["args"] = ["--dummy-arg"]
        elif elem["name"] == "protocol-engine":
            elem["command"] = ["dummy-command"]
        elif elem["name"] == "traffic-engine":
            elem["env"] = {"CUSTOM_ENV": "CUSTOM_VAL"}
    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    custom_configmap_path = "{}".format(CUSTOM_CONFIG_MAP_FILE)
    with open(custom_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    apply_configmap(custom_configmap_path)
    os.remove(custom_configmap_path)


def ixia_c_custom_pods_ok(namespace):
    cmd = "kubectl get pod/otg-controller -n {} -o yaml".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, False)
    yaml_obj = yaml.safe_load(out)
    for elem in yaml_obj["spec"]["containers"]:
        if elem["name"] == "ixia-c":
            assert elem["args"] == ["--dummy-arg"], "Unexpected controller custom args {}".format(elem["args"])
            break

    cmd = "kubectl get pod/otg-port-eth1 -n {} -o yaml".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, False)
    yaml_obj = yaml.safe_load(out)
    for elem in yaml_obj["spec"]["containers"]:
        if elem["name"] == "otg-port-eth1-protocol-engine":
            assert elem["command"] == ["dummy-command"], "Unexpected port custom command {}".format(elem["command"])
        elif elem["name"] == "otg-port-eth1-traffic-engine":
            for envElem in elem["env"]:
                if envElem["name"] == "CUSTOM_ENV":
                    assert envElem["value"] == "CUSTOM_VAL", "Unexpected port custom env {}".format(envElem["value"])
                    return

            assert False, "Expected port custom env CUSTOM_ENV entry not found"


def load_init_configmap():
    print("Loading Init Container Config...")
    cmd = "cat {}".format(
        IXIA_CONFIG_MAP_FILE
    )
    out, _ = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    init_cont = {"name": "init-wait",
                 "path": "networkop/init-wait", "tag": "latest"}
    json_obj["images"].append(init_cont)
    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    init_configmap_path = "{}".format(INIT_CONFIG_MAP_FILE)
    with open(init_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    apply_configmap(init_configmap_path)
    os.remove(init_configmap_path)


def reset_configmap():
    print("Reset Configmap...")
    apply_configmap(IXIA_CONFIG_MAP_FILE)


def load_bad_configmap(bad_component, update_release=False):
    print("Loading Bad Config...")
    cmd = "cat ./{}".format(
        IXIA_CONFIG_MAP_FILE
    )
    out, _ = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    for elem in json_obj["images"]:
        if update_release and elem["name"] == "controller":
            json_obj["release"] = elem["tag"]

        if elem["name"] == bad_component:
            elem["tag"] = "DUMMY"
            break

    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    bad_configmap_path = "{}".format(BAD_CONFIG_MAP_FILE)
    with open(bad_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)
    apply_configmap(bad_configmap_path)
    os.remove(bad_configmap_path)


def load_liveness_configmap(probe):
    print("Loading custom liveness config...")
    cmd = "cat ./{}".format(
        IXIA_CONFIG_MAP_FILE
    )
    out, _ = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    for elem in json_obj["images"]:
        if elem["name"] in probe.keys():
            for key in probe[elem["name"]]:
                elem[key] = probe[elem["name"]][key]
    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    custom_configmap_path = "{}".format(CUSTOM_CONFIG_MAP_FILE)
    print(yaml_obj)
    with open(custom_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    apply_configmap(custom_configmap_path)
    os.remove(custom_configmap_path)


def load_min_resource_configmap(resource):
    print("Loading custom min resource config...")
    cmd = "cat ./{}".format(
        IXIA_CONFIG_MAP_FILE
    )
    out, _ = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    for elem in json_obj["images"]:
        if elem["name"] in resource.keys():
            elem["min-resource"] = dict()
            for key in resource[elem["name"]]:
                elem["min-resource"][key] = resource[elem["name"]][key]
    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    custom_configmap_path = "{}".format(CUSTOM_CONFIG_MAP_FILE)
    with open(custom_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    apply_configmap(custom_configmap_path)
    os.remove(custom_configmap_path)


def load_license_configmap(lic_addr, lic_path, lic_tag):
    print("Loading license config...")
    cmd = "cat ./{}".format(
        BASIC_IXIA_CONFIG_MAP_FILE
    )
    out, _ = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    if lic_addr != "":
        for elem in json_obj["images"]:
            if elem["name"] == "controller":
                elem["env"] = dict()
                elem["env"]["LICENSE_SERVERS"] = lic_addr
                break
    if lic_path != "":
        lic_elem = {
            "name": "license-server",
            "path": lic_path,
            "tag": lic_tag
        }
        json_obj["images"].append(lic_elem)

    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    license_configmap_path = "{}".format(CUSTOM_CONFIG_MAP_FILE)
    with open(license_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    apply_configmap(license_configmap_path)
    os.remove(license_configmap_path)


def load_license_secrets(lic_addr, lic_image):
    print("Loading license secrets...")
    data = " generic license-server --from-literal="
    if lic_addr != "":
        secret_data = data + "addresses=\"" + lic_addr + "\""
        create_secret("ixiatg-op-system", secret_data)
    elif lic_image != "":
        secret_data = data + "image=\"" + lic_image + "\""
        create_secret("ixiatg-op-system", secret_data)


def remove_license_secrets():
    print("Deleting license secrets...")
    cmd = "kubectl delete secret license-server -n ixiatg-op-system"
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        # Ignore exception when secret not present
        return
    else:
        print("Secret license-server deleted inside kind container")


def seconds_elapsed(start_seconds):
    return int(round(time.time() - start_seconds))


def timed_out(start_seconds, timeout):
    return seconds_elapsed(start_seconds) > timeout


def wait_for(func, condition_str, interval_seconds=None, timeout_seconds=None):
    """
    Keeps calling the `func` until it returns true or `timeout_seconds` occurs
    every `interval_seconds`. `condition_str` should be a constant string
    implying the actual condition being tested.

    Usage
    -----
    If we wanted to poll for current seconds to be divisible by `n`, we would
    implement something similar to following:
    ```
    import time
    def wait_for_seconds(n, **kwargs):
        condition_str = 'seconds to be divisible by %d' % n

        def condition_satisfied():
            return int(time.time()) % n == 0

        poll_until(condition_satisfied, condition_str, **kwargs)
    ```
    """
    if interval_seconds is None:
        interval_seconds = 5
    if timeout_seconds is None:
        timeout_seconds = 30
    start_seconds = int(time.time())

    print('\n\nWaiting for %s ...' % condition_str)
    while True:
        if func():
            print('Done waiting for %s' % condition_str)
            break
        if timed_out(start_seconds, timeout_seconds):
            msg = 'Time out occurred while waiting for %s' % condition_str
            raise Exception(msg)

        time.sleep(interval_seconds)


def pods_count_ok(exp_pods, namespace):
    cmd = "kubectl get pods -n {} | grep -v RESTARTS | wc -l".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, False)
    out = out.split('\n')
    actual_pods = int(out[0])
    print("Actual pods: {} - Expected: {}".format(
        actual_pods,
        exp_pods

    ))
    return exp_pods == actual_pods


def pod_status_ok(namespace, pod, status):
    cmd = "kubectl get pod/{} -n {} | grep {} | wc -l".format(
        pod, namespace, status
    )
    out, _ = exec_shell(cmd, True, False)
    out = out.split('\n')
    actual_pods = int(out[0])
    print("Actual pods: {} - Expected: 1".format(
        actual_pods
    ))
    return actual_pods == 1


def containers_count_ok(num_containers, pod, namespace):
    cmd = "kubectl get pod/{} -n {} | grep -v RESTARTS".format(
        pod, namespace
    )
    out, _ = exec_shell(cmd, True, False)
    out = out.split()
    exp_containers = "{}/{}".format(num_containers, num_containers)
    assert len(out) >= 5 and out[2] == "Running" and out[1] == exp_containers, \
             "Unexpected controller status or container count found"


def svcs_count_ok(exp_svcs, namespace):
    cmd = "kubectl get svc -n {} | grep -v TYPE | wc -l".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, False)
    out = out.split('\n')
    actual_svcs = int(out[0])
    print("Actual services: {} - Expected: {}".format(
        actual_svcs,
        exp_svcs

    ))
    return exp_svcs == actual_svcs


def pods_status_ok(exp_pods, namespace):
    cmd = "kubectl get pods -n {} | grep Running | wc -l".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, False)
    out = out.split('\n')
    actual_pods = int(out[0])
    print("Actual Running pods: {} - Expected: {}".format(
        actual_pods,
        exp_pods

    ))
    return exp_pods == actual_pods


def pod_exists(podname, namespace):
    cmd = "kubectl describe pods/{} -n {}".format(
        podname, namespace
    )
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        return False
    else:
        return True


def svc_exists(svcname, namespace):
    cmd = "kubectl describe svc/{} -n {}".format(
        svcname, namespace
    )
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        return False
    else:
        return True


def get_operator_restart_count():
    cmd = "kubectl get pods -n ixiatg-op-system -o 'jsonpath={.items[0].status.containerStatuses[?(@.name==\"manager\")].restartCount}'"  # noqa
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        raise Exception("Operator pod not found!!!")
    else:
        restart_count = int(out.split('\n')[0])
        print("Operator Pod Restart Count: {}".format(
            restart_count
        ))

        return restart_count


def ixia_c_services_ok(namespace, exp_services=[]):
    print("[Namespace:{}]Verifying services count in KNE topology".format(
        namespace
    ))
    exp_svc_count = len(exp_services)
    wait_for(
        lambda: svcs_count_ok(exp_svc_count, namespace),
        'services count to be as expected',
        timeout_seconds=300
    )

    print("[Namespace:{}]Verifying individual services in KNE topology".format(
        namespace
    ))
    for exp_svc in exp_services:
        assert svc_exists(
            exp_svc,
            namespace
        ), "Service {} - not found!!!".format(
            exp_svc
        )


def ixia_c_pods_ok(namespace, exp_pods=[], count=True,
                   health=True, individual=True):
    exp_pod_count = len(exp_pods)
    if count:
        print("[Namespace:{}]Verifying pods count in KNE topology".format(
            namespace
        ))
        wait_for(
            lambda: pods_count_ok(exp_pod_count, namespace),
            'pods count to be as expected',
            timeout_seconds=300
        )

    if health:
        print("[Namespace:{}]Verifying pods status in KNE topology".format(
            namespace
        ))
        wait_for(
            lambda: pods_status_ok(exp_pod_count, namespace),
            'pods status to be as expected',
            timeout_seconds=300
        )

    if individual:
        print("[Namespace:{}]Verifying individual pods in KNE topology".format(
            namespace
        ))
        for exp_pod in exp_pods:
            assert pod_exists(
                exp_pod,
                namespace
            ), "Pod {} - not found!!!".format(
                exp_pod
            )


def ixia_c_pod_status_match(namespace, pod, status='Running'):
    print("[Namespace:{}]Verifying pod {} status {} in KNE topology".format(
        namespace, pod, status
    ))
    wait_for(
        lambda: pod_status_ok(namespace, pod, status),
        'pod status to be as expected',
        timeout_seconds=300
    )


def ixia_c_operator_ok(prev_op_rscount):
    print("Verifying Operator pod status ....")
    op_rscount = get_operator_restart_count()
    total_restarts = op_rscount - prev_op_rscount
    assert total_restarts == 0, \
        "Operator restarts {} times during deploying KNE topo" .format(
            total_restarts
        )
    return op_rscount


def check_probe_data(cont, pod, namespace, liveness=True, enabled=True, delay=0, period=0, failure=0):
    base_cmd = "'jsonpath={.spec.containers[?(@.name==\"" + cont + "\")]."
    if liveness:
        base_cmd = base_cmd + "livenessProbe"
    else:
        base_cmd = base_cmd + "startupProbe"
    base_cmd = "kubectl get pod/{} -n {} -o ".format(pod, namespace) + base_cmd
    cmd = base_cmd + "}'"
    out, _ = exec_shell(cmd, True, True)
    if enabled:
        assert out != "", "Expected liveness to be enabled; no liveness data found"
    else:
        assert out == "", "Expected liveness to be disabled; liveness data non-empty"
        return
    res = json.loads(out)
    if delay != 0:
        key = 'initialDelaySeconds'
        assert key in res.keys() and delay == res[key], "InitialDelaySeconds mismatch, expected {}, found {}".format(delay, res[key])
    if period != 0:
        key = 'periodSeconds'
        assert key in res.keys() and period == res[key], "PeriodSeconds mismatch, expected {}, found {}".format(period, res[key])
    if failure != 0:
        key = 'failureThreshold'
        assert key in res.keys() and failure == res[key], "FailureThreshold mismatch, expected {}, found {}".format(failure, res[key])


def check_env_data(cont, pod, namespace, name, value):
    base_cmd = "'jsonpath={.spec.containers[?(@.name==\"" + cont + "\")].env"
    base_cmd = "kubectl get pod/{} -n {} -o ".format(pod, namespace) + base_cmd
    cmd = base_cmd + "}'"
    out, _ = exec_shell(cmd, True, True)
    res = json.loads(out)
    for elem in res:
        if elem['name'] == name:
            if elem['value'] == value:
                return
            else:
                raise Exception("Mismatch in environment variable '" + name + "' value")
    raise Exception("Failed to find environment variable '" + name + "'")


def check_min_resource_data(cont, pod, namespace, memory="", cpu=""):
    base_cmd = "'jsonpath={.spec.containers[?(@.name==\"" + cont + "\")].resources.requests"
    base_cmd = "kubectl get pod/{} -n {} -o ".format(pod, namespace) + base_cmd
    cmd = base_cmd + "}'"
    out, _ = exec_shell(cmd, True, True)
    print(out)
    res = json.loads(out)
    if memory != "":
        key = 'memory'
        assert key in res.keys() and memory == res[key], "Memory resource mismatch, expected {}, found {}".format(memory, res[key])
    if cpu != "":
        key = 'cpu'
        assert key in res.keys() and cpu == res[key], "Cpu resource mismatch, expected {}, found {}".format(cpu, res[key])


def delete_config(config):
    config_path = get_kne_config_path(config)
    cmd = "rm -rf {}".format(
        config_path
    )
    out, _ = exec_shell(cmd, True, True)
    if out is None:
        raise Exception('Failed to delete kne config ...')
    else:
        print("KNE topo config deleted :{}".format(
            config_path
        ))


def time_taken_for_pods_to_be_ready(namespace, exp_pods):
    start = time.time()
    exp_pod_count = len(exp_pods)
    print("[Namespace:{}]Waiting for pods to be Running...".format(
        namespace
    ))
    wait_for(
        lambda: pods_status_ok(exp_pod_count, namespace),
        'pods status to be as expected',
        timeout_seconds=300,
        interval_seconds=0.1
    )
    elapsed_time = time.time() - start
    return elapsed_time


def time_taken_for_pods_to_be_terminated(namespace, exp_pods):
    start = time.time()
    exp_pod_count = len(exp_pods)
    print("[Namespace:{}]Waiting for pods to be Terminated...".format(
        namespace
    ))
    wait_for(
        lambda: pods_count_ok(exp_pod_count, namespace),
        'pods count to be as expected',
        timeout_seconds=300,
        interval_seconds=0.1
    )
    elapsed_time = time.time() - start
    return elapsed_time


def time_ok(time_taken, exp_time, tolerance):
    exp_max_time = exp_time * (1 + tolerance // 100)
    if time_taken <= exp_max_time:
        return True
    return False


def get_topologies(namespace):
    print("Getting meshnet topologies ...")
    actual_topologies = []
    cmd = "kubectl get topologies -o yaml -n {}".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, True)
    yaml_obj = yaml.safe_load(out)
    for item in yaml_obj["items"]:
        topology = {
            "metadata": {
                "name": item["metadata"]["name"],
                "namespace": item["metadata"]["namespace"],
            },
            "spec": item["spec"],
        }
        actual_topologies.append(topology)
    return actual_topologies



def get_ixiatgs(namespace):
    print("Getting ixiatgs ...")
    actual_ixiatgs = []
    cmd = "kubectl get ixiatgs -o yaml -n {}".format(
        namespace
    )
    out, _ = exec_shell(cmd, True, True)
    yaml_obj = yaml.safe_load(out)
    print(yaml_obj)
    for item in yaml_obj["items"]:
        ixiatg = {
            "metadata": {
                "name": item["metadata"]["name"],
                "namespace": item["metadata"]["namespace"],
            },
            "spec": item["spec"],
            "status": item["status"]
        }
        actual_ixiatgs.append(ixiatg)

    print(actual_ixiatgs)
    return actual_ixiatgs


def get_ingress_ip(namespace, service):
    cmd = "kubectl get svc/" + service + " -n " + namespace + " -o 'jsonpath={.status.loadBalancer}'"
    out, _ = exec_shell(cmd, True, True)
    yaml_obj = yaml.safe_load(out)
    ingress_ip = yaml_obj['ingress'][0]['ip']
    print("Ingress IP of service: {} in namespace: {}".format(
        service,
        namespace
    ))
    return ingress_ip


def get_ingress_mapping(namespace, services):
    ingress_map = dict()
    for service in services:
        print("Finding ingress mapping for service: {} in namespace: {}".format(
            service, namespace
        ))
        ingress_map[service] = get_ingress_ip(namespace, service)

    return ingress_map


def check_socket_connection(host, port):
    retry = 5
    attempt = 1
    while attempt <= retry:
        try:
            print("attempt: {}".format(attempt))
            s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            s.connect((host,port))
            s.close()
            print("Socket connection for {}:{} is alive....".format(host, port))
            return True
        except Exception as e:
            attempt += 1
            time.sleep(1)
    print("Socket connection for {}:{} is dead....".format(host, port))
    return False


def socket_alive(exp_svcs, svc_ing_map):
    for exp_svc, ports in exp_svcs.items():
        for port in ports:
            print("Checking socket is alive for service {} on port {}...".format(
                exp_svc,
                port
            ))
            assert check_socket_connection(svc_ing_map[exp_svc], port), "socket is dead for service {} on port {}...".format(
                exp_svc, port
            )


def delete_namespace(namespace):
    cmd = "kubectl delete namespace {}".format(
        namespace
    )
    exec_shell(cmd, True, True)
    

