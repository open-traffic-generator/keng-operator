import os
import subprocess
import time
import json
import yaml

SUDO_USER = 'root'

# path to dir containing kne configurations relative root dir
CONFIGS_DIR = 'kne_configs'
BAD_CONFIGMAP_FILE = 'bad-configmap.yaml'
INIT_CONFIGMAP_FILE = 'init-configmap.yaml'
IXIA_CONFIGMAP_FILE = 'ixia-configmap.yaml'

KIND_SINGLE_NODE_NAME = 'kind-control-plane'

def exec_shell(cmd, sudo=True, check_return_code=True):
    """
    Executes a command in native shell and returns output as str on success or,
    None on error.
    """
    if not sudo:
        cmd = 'sudo -u ' + SUDO_USER + ' ' + cmd

    print('Executing `%s` ...' % cmd)
    p = subprocess.Popen(
        cmd.encode('utf-8', errors='ignore'),
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, shell=True
    )
    out, _ = p.communicate()
    out = out.decode('utf-8', errors='ignore')

    print('Output:\n%s' % out)

    if check_return_code:
        if p.returncode == 0:
            return out
        return None
    else:
        return out


def get_kne_config_path(config_name):
    sep = os.path.sep
    return sep.join([
        '.',
        CONFIGS_DIR,
        config_name
    ])

def copy_file_to_kind(filepath):
    cmd = "docker cp {} {}:/".format(
        filepath, KIND_SINGLE_NODE_NAME
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        raise Exception("Failed to copy file {} to kind container".format(
            filepath
        ))
    else:
        print("{} copied inside kind container".format(
            filepath
        ))

def delete_file_from_kind(filepath):
    cmd = "docker exec -t {} rm -rf ./{}".format(
        KIND_SINGLE_NODE_NAME, filepath
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        raise Exception("Failed to delete file {} from kind container".format(
            filepath
        ))
    else:
        print("{} deleted from kind container".format(
            filepath
        ))


def topology_deleted(namespace):
    cmd = "docker exec -t {} kubectl get topology -n {}".format(
        KIND_SINGLE_NODE_NAME,
        namespace
    )
    out = exec_shell(cmd, True, False)
    out = out.split('\n')
    if 'No resources found' in out[0]:
        return True
    return False
    
    
def create_kne_config(config_name, namespace):
    config_path = get_kne_config_path(config_name)
    copy_file_to_kind(config_path)
    # Ensure topology was deleted from past run
    wait_for(
        lambda: topology_deleted(namespace),
        'ensured topology does not exists',
        timeout_seconds=120
    )
    cmd = "docker exec -t {} ./kne_cli create ./{}".format(
        KIND_SINGLE_NODE_NAME,
        config_name
    )
    exec_shell(cmd, True, False)


def delete_kne_config(config_name, namespace):
    cmd = "docker exec -t {} ./kne_cli delete ./{}".format(
        KIND_SINGLE_NODE_NAME,
        config_name
    )
    exec_shell(cmd, True, False)
    wait_for(
        lambda: topology_deleted(namespace),
        'topology deleted',
        timeout_seconds=30
    )
    delete_file_from_kind(config_name)


def apply_configmap(configmap):
    cmd = "docker exec -t {} kubectl apply -f /{}".format(
        KIND_SINGLE_NODE_NAME,
        configmap
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        raise Exception("Failed to apply configmap {} inside kind container".format(
            configmap
        ))
    else:
        print("configmap {} applied inside kind container".format(
            configmap
        ))

def unload_init_configmap():
    print("Unloading init container Config...")
    delete_file_from_kind(INIT_CONFIGMAP_FILE)
    apply_configmap(IXIA_CONFIGMAP_FILE)


def load_init_configmap():
    print("Loading Init Container Config...")
    cmd = "docker exec -t {} cat ./{}".format(
        KIND_SINGLE_NODE_NAME,
        IXIA_CONFIGMAP_FILE
    )
    out = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    init_cont = {"name":"init-wait", "path":"networkop/init-wait", "tag":"latest"}
    json_obj["images"].append(init_cont)
    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    init_configmap_path = "/tmp/{}".format(INIT_CONFIGMAP_FILE)
    with open(init_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    copy_file_to_kind(init_configmap_path)
    os.remove(init_configmap_path)

    apply_configmap(INIT_CONFIGMAP_FILE)


def unload_bad_configmap():
    print("Unloading Bad Config...")
    delete_file_from_kind(BAD_CONFIGMAP_FILE)
    apply_configmap(IXIA_CONFIGMAP_FILE)


def load_bad_configmap(bad_component, update_release=False):
    print("Loading Bad Config...")
    cmd = "docker exec -t {} cat ./{}".format(
        KIND_SINGLE_NODE_NAME,
        IXIA_CONFIGMAP_FILE
    )
    out = exec_shell(cmd, False, True)
    yaml_obj = yaml.safe_load(out)
    json_obj = json.loads(yaml_obj["data"]["versions"])
    for elem in json_obj["images"]:
        if update_release and elem["name"] == "controller":
            json_obj["release"] = elem["tag"]
            
        if elem["name"] == bad_component:
            elem["tag"] = "DUMMY"
            break

    yaml_obj["data"]["versions"] = json.dumps(json_obj)
    bad_configmap_path = "/tmp/{}".format(BAD_CONFIGMAP_FILE)
    with open(bad_configmap_path, "w") as yaml_file:
        yaml.dump(yaml_obj, yaml_file)

    copy_file_to_kind(bad_configmap_path)
    os.remove(bad_configmap_path)

    apply_configmap(BAD_CONFIGMAP_FILE)

        
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
    cmd = "docker exec -t {} /bin/bash -c 'kubectl get pods -n {} | grep -v RESTARTS | wc -l'".format(
        KIND_SINGLE_NODE_NAME, namespace
    )
    out = exec_shell(cmd, True, False)
    out = out.split('\n')
    if 'No resources found' in out[0]:
        actual_pods = int(out[1])
    else:
        actual_pods = int(out[0])
    print("Actual pods: {} - Expected: {}".format(
        actual_pods,
        exp_pods

    ))
    return exp_pods == actual_pods

def svcs_count_ok(exp_svcs, namespace):
    cmd = "docker exec -t {} /bin/bash -c 'kubectl get svc -n {} | grep -v TYPE | wc -l'".format(
        KIND_SINGLE_NODE_NAME, namespace
    )
    out = exec_shell(cmd, True, False)
    out = out.split('\n')
    if 'No resources found' in out[0]:
        actual_svcs = int(out[1])
    else:
        actual_svcs = int(out[0])
    print("Actual services: {} - Expected: {}".format(
        actual_svcs,
        exp_svcs

    ))
    return exp_svcs == actual_svcs


def pods_status_ok(exp_pods, namespace):
    cmd = "docker exec -t {} /bin/bash -c 'kubectl get pods -n {} | grep Running | wc -l'".format(
        KIND_SINGLE_NODE_NAME, namespace
    )
    out = exec_shell(cmd, True, False)
    out = out.split('\n')
    if 'No resources found' in out[0]:
        actual_pods = int(out[1])
    else:
        actual_pods = int(out[0])
    print("Actual Running pods: {} - Expected: {}".format(
       actual_pods,
        exp_pods

    ))
    return exp_pods == actual_pods

def pod_exists(podname, namespace):
    cmd = "docker exec -t {} /bin/bash -c 'kubectl describe pods/{} -n {}'".format(
        KIND_SINGLE_NODE_NAME, podname, namespace
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        return False
    else:
        return True


def svc_exists(svcname, namespace):
    cmd = "docker exec -t {} /bin/bash -c 'kubectl describe svc/{} -n {}'".format(
        KIND_SINGLE_NODE_NAME, svcname, namespace
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        return False
    else:
        return True


def get_operator_restart_count():
    cmd = "docker exec -t " + KIND_SINGLE_NODE_NAME + " kubectl get pods -n ixiatg-op-system -o 'jsonpath={.items[0].status.containerStatuses[?(@.name==\"manager\")].restartCount}'"
    out = exec_shell(cmd, True, True)
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


def ixia_c_pods_ok(namespace, exp_pods=[], count=True, health=True, individual=True):
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


def ixia_c_operator_ok(prev_op_rscount):
    print("Verifying Operator pod status ....")
    op_rscount = get_operator_restart_count()
    total_restarts = op_rscount - prev_op_rscount
    assert total_restarts == 0, \
        "Operator restarts {} times during deploying KNE topo" .format(
            total_restarts
        )
    return op_rscount


def generate_rest_config_from_temaplate(config, ixia_c_release):
    template_config_path = get_kne_config_path(
        'template_' + config
    )
    config_path = get_kne_config_path(config)
    cmd = "cat {} | sed \"s/IXIA_C_RELEASE/{}/g\" | tee {} > /dev/null".format(
        template_config_path,
        ixia_c_release,
        config_path
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        raise Exception('Failed to generate rest config from template')
    else:
        print("Rest KNE topo config generated from template :{}".format(
            config_path
        ))


def delete_config(config):
    config_path = get_kne_config_path(config)
    cmd = "rm -rf {}".format(
        config_path
    )
    out = exec_shell(cmd, True, True)
    if out is None:
        raise Exception('Failed to delete kne config ...')
    else:
        print("KNE topo config deleted :{}".format(
            config_path
        ))


def generate_opts_json_from_template(namespcae):
    opts_json = 'opts.json'
    template_json = 'template-opts.json'
    if os.path.exists(opts_json):
        os.remove(opts_json)
    
    cmd = "cat {} | sed -E 's/IXIA_C_NAMESPACE/{}/g' | tee {} > /dev/null".format(
        template_json,
        namespcae,
        opts_json
    )

    print(cmd)
    out = exec_shell(cmd, True, True)
    if out is None:
        raise Exception('Failed to generate opts.json from template')
    else:
        print("opts.json generated from template for namespcae: {}".format(
            namespcae
        ))


def delete_opts_json():
    opts_json = 'opts.json'
    if os.path.exists(opts_json):
        os.remove(opts_json)
    print("opts.json deleted ...")
    delete_file_from_kind(opts_json)


def copy_opts_to_testclient():
    local_opts = "./opts.json"
    copy_file_to_kind(local_opts)
    cp_cmd = "docker exec -t {} kubectl cp ./opts.json ixia-c-test-client:/home/keysight/athena/tests/go/tests/opts.json".format(
        KIND_SINGLE_NODE_NAME
    )
    out = exec_shell(cp_cmd, True, True)
    if out is None:
        raise Exception('Failed to copy opts.json to ixia-c-test-client')
    else:
        print("opts.json copied to ixia-c-test-client")

def run_e2e_test_from_client(report, testcase=None, tags="sanity"):
    print("Running e2e test case ...")
    if os.path.exists(report):
        os.remove(report)

    test_run_cmd = "go test -timeout 24h -tags={} -v".format(
        tags
    )
    if testcase:
        test_run_cmd = "go test -run={} -tags={} -v".format(
            testcase, tags
        )
    cp_cmd = 'docker exec -t '+ KIND_SINGLE_NODE_NAME +' kubectl exec -t ixia-c-test-client -- bash -c "cd go/tests; '+ test_run_cmd + '" | tee '+ report
    out = exec_shell(cp_cmd, True, False)

def check_e2e_test_status(report, expected_pass_rate=100):
    print("Checking e2e test status ...")
    cmd = "cat {} | grep -c '=== RUN'".format(
        report
    )
    out = exec_shell(cmd,True, False)
    total_count = int(out)

    cmd = "cat {} | grep -c 'PASS:'".format(
        report
    )
    out = exec_shell(cmd,True, False)
    pass_count = int(out)

    print('Total Count : {} - Pass Count: {}'.format(
        total_count,
        pass_count
    ))
    
    pass_rate = (pass_count / total_count) * 100

    print('Actual Pass Rate : {} - Expected: {}'.format(
        pass_rate,
        expected_pass_rate
    ))

    if os.path.exists(report):
        os.remove(report)
    if total_count != 0 and pass_rate >= expected_pass_rate:
        return True
    return False


def ixia_c_e2e_test_ok(namespace, testcase=None, tags="sanity", expected_pass_rate=100):
    print("[Namespace: {}]Generating local opts.json from template".format(
        namespace
    ))
    generate_opts_json_from_template(namespace)

    print("[Namespace: {}]Copying local opts.json to test-client".format(
        namespace
    ))
    copy_opts_to_testclient()

    test_report = "report-{}-e2e.txt".format(
        namespace
    )
    print("[Namespace: {}]Running E2E tests from test-client".format(
        namespace
    ))
    run_e2e_test_from_client(test_report, testcase, tags)

    print("[Namespace: {}]Analyzing E2E test results".format(
        namespace
    ))
    assert check_e2e_test_status(test_report, expected_pass_rate), "E2E test case failed!!!"

    print("[Namespace: {}]Deleting local opts.json".format(
        namespace
    ))
    delete_opts_json()


def arista_sshable_ok(arista_pods, namespace):
    print("[Namespace: {}]Verifying arista pods to be sshable".format(
        namespace
    ))
    for pod in arista_pods:
        print(pod)
        wait_for(
            lambda: is_arista_ssh_reachable(pod, namespace),
            'arista pods to be sshable',
            timeout_seconds=300
        )

def is_arista_ssh_reachable(pod, namespcae):
    cmd = "docker exec -t " + KIND_SINGLE_NODE_NAME + " kubectl get services service-" + pod + " -n " + namespcae + " -o 'jsonpath={.spec.ports[?(@.name==\"ssh\")].nodePort}'"
    out = exec_shell(cmd, True, True)
    if out is not None:
        nodeport = out.split('\n')[0]
        print("namespace: {}, pod: {} - nodeport: {}".format(
            namespcae,
            pod,
            nodeport
        ))

        ssh_cmd = "docker exec -t {} ssh -p {} -o StrictHostKeyChecking=no -o \"UserKnownHostsFile /dev/null\" admin@localhost echo ok".format(
            KIND_SINGLE_NODE_NAME,
            nodeport
        )
        out = exec_shell(ssh_cmd, True, True)
        print(out)
        if out is not None:
            print('namespace: {}, pod: {} - sshable'.format(
                namespcae,
                pod
            ))
            return True
        else:
            print('namespace: {}, pod: {} - not sshable yet'.format(
                namespcae,
                pod
            ))
            return False


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
