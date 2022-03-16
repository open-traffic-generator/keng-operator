import argparse
import csv
import json
import os
import platform
import re
import subprocess
import sys
import traceback

SUDO_USER = 'root'

SANITY_SETTINGS = {
    "required_artifacts": [
        "ixia-c-operator.tar.gz",
        "ixiatg-operator.yaml",
        "template-ixia-configmap.yaml",
        "operator-cicd-deploy.sh",
        "operator-tests.tar.gz",
        "template-ixia-c-test-client.yaml",
    ],
    "deploy_cmd": "chmod u+x ./operator-cicd-deploy.sh && ./operator-cicd-deploy.sh load_images && ./operator-cicd-deploy.sh deploy && ./operator-cicd-deploy.sh deploy_operator_tests && echo success", # noqa
    "cleanup_cmd": "chmod u+x ./operator-cicd-deploy.sh && ./operator-cicd-deploy.sh delete && ./operator-cicd-deploy.sh delete_images && echo success", # noqa
    "sanity_cmd": "{} -m pytest --html={} --self-contained-html --ixia_c_release={} ./py/ -m {} --capture sys --metadata-from-json '{}'", # noqa
}


def exec_shell(cmd, logger, sudo=True, check_return_code=True):
    """
    Executes a command in native shell and returns output as str on success or,
    None on error.
    """
    if not sudo:
        cmd = 'sudo -u ' + SUDO_USER + ' ' + cmd

    logger.debug('Executing `%s` ...' % cmd)
    p = subprocess.Popen(
        cmd.encode('utf-8', errors='ignore'),
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, shell=True
    )
    out, _ = p.communicate()
    out = out.decode('utf-8', errors='ignore')

    logger.debug('Output:\n%s' % out)

    if check_return_code:
        if p.returncode == 0:
            return out
        return None
    else:
        return out


def append_csv_row(dirname, filename, column_names, result_dict):
    """
    creates a new csv with column names if it doesn't exist and appends a
    single row specified by result_dict
    """
    path = os.path.join(dirname, filename)

    with open(path, 'a', newline='') as fp:
        csv_writer = csv.writer(fp)
        if os.path.getsize(path) == 0:
            csv_writer.writerow(column_names)

        csv_writer.writerow([result_dict[key] for key in column_names])


class log(object):
    """
    This class provides logging with pigmentation. All methods can be called
    directly on class itself.
    """

    fd = open('run_operator_cicd.log', 'w+')

    @classmethod
    def style(
        cls, text, color=None, bold=False, underline=False,
        no_fmt='windows' in platform.system().lower(), codes={
            'red': 91, 'green': 92, 'yellow': 93, 'blue': 94,
            'bold': 1, 'underline': 4
        }
    ):
        """
        Embeds ansi escape codes in input string, corresponding to text format
        options provided in arguments and returns final string. Returns
        unformatted string on windows.
        """
        if no_fmt:
            return text

        code = '\033['
        if color:
            code += '%d;' % codes[color]
        if bold:
            code += '%d;' % codes['bold']
        if underline:
            code += '%d;' % codes['underline']

        if code.endswith(';'):
            return code.rstrip(';') + 'm' + text + '\033[0m'
        else:
            return text

    @classmethod
    def bold(cls, msg):
        return cls.style(msg, bold=True)

    @classmethod
    def blue(cls, msg, bold=True, underline=False):
        return cls.style(msg, color='blue', bold=bold, underline=underline)

    @classmethod
    def green(cls, msg, bold=True, underline=False):
        return cls.style(msg, color='green', bold=bold, underline=underline)

    @classmethod
    def yellow(cls, msg, bold=True, underline=False):
        return cls.style(msg, color='yellow', bold=bold, underline=underline)

    @classmethod
    def red(cls, msg, bold=True, underline=False):
        return cls.style(msg, color='red', bold=bold, underline=underline)

    @classmethod
    def info(cls, msg, newline=True):
        end = '\n' if newline else ''
        sys.stdout.write(msg + end)
        sys.stdout.flush()
        cls.fd.write(msg + end)
        cls.fd.flush()

    @classmethod
    def debug(cls, msg, newline=True):
        end = '\n' if newline else ''
        msg = 'DEBUG: ' + msg
        sys.stdout.write(msg + end)
        sys.stdout.flush()

        cls.fd.write(msg + end)
        cls.fd.flush()

    @classmethod
    def error(cls, msg):
        cls.info("\n" + cls.red(msg) + "\n")


class RunIxiaCOperatorE2E:
    def __init__(self):
        self.build = None
        self.ixia_c_release = None
        self.test = False
        self.mark = None
        self.clean = False
        self.cwd = os.getcwd()
        self.test_meta_data = dict()
        self.logger = log

    def get_cmd_line_options(self):
        try:
            parser = argparse.ArgumentParser(
                description="--- Auto Ixia-C-Operator E2E Regression Run Tool help ---") # noqa
            parser.add_argument(
                '-build',
                dest='build',
                default=self.build,
                help="Enter the Ixia-C-Operator build version")
            parser.add_argument(
                '-ixia_c_release',
                dest='ixia_c_release',
                default=self.ixia_c_release,
                help="Enter the Ixia-C Release")
            parser.add_argument(
                '--test',
                dest='test',
                action='store_true',
                default=self.test,
                help="Run e2e tests")
            parser.add_argument(
                '-mark', dest='mark',
                default=self.mark,
                help="Run e2e tests for given marker")
            parser.add_argument(
                '--clean',
                dest='clean',
                action='store_true',
                default=self.clean,
                help="Cleanup TestBed")

            args = parser.parse_args()

            if len(sys.argv) == 1:
                raise Exception('Incorrect Input provided.')

            args = parser.parse_args()
            for arg in vars(args):
                value = getattr(args, arg)
                if value is not None:
                    setattr(self, arg, value)

        except Exception as e:
            print('Error in get_cmd_line_options : {}'.format(e))
            raise Exception(e)

    def get_artifacts(self):
        try:
            artifacts_to_be_present = SANITY_SETTINGS[
                "required_artifacts"
            ]
            for artifact in artifacts_to_be_present:
                artifact_path = os.path.join(self.cwd, artifact)
                if not os.path.exists(artifact_path):
                    raise Exception("{} : not found".format(artifact_path))

            self.logger.info(
                'Artifacts {} present at : {}'.format(
                    artifacts_to_be_present, self.cwd))
        except Exception as e:
            print(
                "Error in get_artifacts of RunIxiaCOperatorE2E : {}".format(
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def get_distro(self):
        try:
            """
            Name of your Linux distro (in lowercase).
            """
            with open("/etc/issue") as f:
                return f.read().lower().split()[0]
        except Exception as e:
            print("Error in get_distro of RunIxiaCOperatorE2E : {}".format(e))
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def form_test_metadata(self):
        try:
            test_meta_data = dict()
            test_meta_data["Distribution"] = self.get_distro()
            test_meta_data["CPU"] = os.cpu_count()
            total_ram = int(os.popen('free -t -m').readlines()[1].split()[1])
            test_meta_data["RAM"] = str(
                round(total_ram / 1000, 3)) + ' GB'
            test_meta_data["Operator Version"] = self.build
            self.test_meta_data = test_meta_data
            self.logger.info(
                'Template for Meta Data of Test : {} -- Formed'.format(
                    self.test_meta_data))
        except Exception as e:
            print(
                "Error in form_test_metadata of RunIxiaCOperatorE2E : {}".format( # noqa
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def deploy_ixia_c_op(self):
        try:
            self.logger.info('Deploying Ixia-C-Operator')
            cmd = SANITY_SETTINGS['deploy_cmd']
            out = exec_shell(
                cmd, self.logger)
            if out is not None:
                out = out.split('\n')
                if out[-2].strip() == "success":
                    self.logger.info('Ixia-C-Operator deployed successfully')
                else:
                    raise Exception('Failed to deploy Ixia-C-Operator')
            else:
                raise Exception('Failed to deploy Ixia-C-Operator')

        except Exception as e:
            print(
                "Error in deploy_ixia_c_op of RunIxiaCOperatorE2E : {}".format(
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def parse_test_report(self, test_report_name):
        try:
            cp_cmd = "cp ./operator-tests/{} ./".format(
                test_report_name
            )
            exec_shell(cp_cmd, self.logger)
            test_report_location = os.path.join(os.getcwd(), test_report_name)
            if os.path.exists(test_report_location):

                with open(test_report_location, 'r') as file:
                    htmlStr = file.read()

                pass_pattern = r'(?<=<span class="passed">)(.*)(?= passed</span>)' # noqa
                pass_count = int(re.findall(pass_pattern, htmlStr)[0])

                skip_pattern = r'(?<=<span class="skipped">)(.*)(?= skipped</span>)' # noqa
                skip_count = int(re.findall(skip_pattern, htmlStr)[0])

                fail_pattern = r'(?<=<span class="failed">)(.*)(?= failed</span>)' # noqa
                fail_count = int(re.findall(fail_pattern, htmlStr)[0])

                total_count = pass_count + skip_count + fail_count

                pass_rate = int((pass_count / total_count) * 100)
                header_row = ['marker', 'pass rate']
                test_status_row = [self.mark, pass_rate]

                self.logger.info('pass rate for {} : {}'.format(
                    self.mark, pass_rate))

                summary_loaction = os.path.join(
                    os.getcwd(),
                    self.mark + '-summary-' + self.build + '.csv')
                if not os.path.exists(summary_loaction):
                    row_list = []
                    row_list.append(header_row)
                    row_list.append(test_status_row)
                    with open(summary_loaction, 'w', newline="") as csvfile:
                        csvwriter = csv.writer(csvfile, delimiter=",")
                        for row in row_list:
                            csvwriter.writerow(row)
                        csvfile.close()
                else:

                    with open(summary_loaction, 'a') as csvfile:
                        csvwriter = csv.writer(csvfile, delimiter=",")
                        csvwriter.writerow(test_status_row)
                        csvfile.close()

                self.logger.info('csv date dumped to : {}'.format(
                    summary_loaction))

            else:
                raise Exception("{} : not found".format(test_report_location))
        except Exception as e:
            self.logger.error("Error in parse_test_report : {}".format(e))
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def update_overall_sanity_status(self):
        try:
            summary_loaction = os.path.join(
                self.cwd,
                self.mark + '-summary-' + self.build + '.csv')
            if os.path.exists(summary_loaction):
                total_pass_rate = 0
                marker_count = 0
                summary_info = csv.DictReader(open(summary_loaction))
                summary_info_list = []
                for info in summary_info:
                    summary_info_list.append(dict(info))
                for d in summary_info_list:
                    marker_count += 1
                    total_pass_rate += int(d['pass rate'])

                entire_sanity_pass_rate = total_pass_rate // marker_count
                overall_status = ["All", entire_sanity_pass_rate]

                self.logger.info('pass rate for entire sanity : {}'.format(
                    entire_sanity_pass_rate))

                with open(summary_loaction, 'a') as csvfile:
                    csvwriter = csv.writer(csvfile, delimiter=",")
                    csvwriter.writerow(overall_status)
                    csvfile.close()

                self.logger.info('csv date dumped to : {}'.format(
                    summary_loaction))
            else:
                raise Exception("{} : not found".format(summary_loaction))

        except Exception as e:
            print(
                "Error in update_overall_sanity_status : {}".format(e))
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def execute_sanity(self):
        try:
            self.logger.info("Sanity run meta data : {}".format(
                self.test_meta_data))
            test_report_name = 'reports-' + \
                self.build + '.html'
            test_report_location = os.path.join(os.getcwd(), test_report_name)
            if os.path.exists(test_report_location):
                self.logger.info('Removing {} before running sanity'.format(
                    test_report_location))
                ok = exec_shell(
                    'rm {}'.format(test_report_location), self.logger)
                if ok is None:
                    raise Exception('Failed to remove : {}'.format(
                        test_report_location))
            self.logger.info(
                'Starting sanity run for Ixia-C-Operator')
            sanity_run_command = SANITY_SETTINGS["sanity_cmd"].format(
                sys.executable,
                test_report_name,
                self.ixia_c_release,
                self.mark,
                json.dumps(
                    self.test_meta_data
                )
            )
            sanity_run_command = "cd operator-tests && " + sanity_run_command
            exec_shell(sanity_run_command, self.logger)

            self.parse_test_report(test_report_name)
        except Exception as e:
            print(
                "Error in execute_sanity of RunIxiaCOperatorE2E : {}".format(
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def cleanup_ixia_c_op(self):
        try:
            self.logger.info(
                'Cleaning up Ixia-C-Operator')

            cmd = SANITY_SETTINGS['cleanup_cmd']
            out = exec_shell(
                cmd, self.logger)
            if out is not None:
                out = out.split('\n')
                if out[-2].strip() == "success":
                    self.logger.info('Ixia-C-Operator clean up successfully')
                else:
                    raise Exception('Failed to clean up Ixia-C-Operator')
            else:
                raise Exception('Failed to clean up Ixia-C-Operator')

        except Exception as e:
            print(
                "Error in cleanup_ixia_c_op of RunIxiaCOperatorE2E: {}".format(
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def remove_file(self, file_name):
        try:
            ok = exec_shell('rm {}'.format(file_name), self.logger)
            if ok is not None:
                self.logger.info("{} : removed".format(file_name))
            else:
                raise Exception("{} : failed to remove".format(file_name))
        except Exception as e:
            print("Error in remove_file of RunIxiaCOperatorE2E : {}".format(e))
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def remove_folder(self, folder):
        try:
            ok = exec_shell('rm -rf {}'.format(folder), self.logger)
            if ok is not None:
                self.logger.info("{} : removed".format(folder))
            else:
                raise Exception("{} : failed to remove".format(folder))
        except Exception as e:
            print(
                "Error in remove_folder of RunIxiaCOperatorE2E : {}".format(
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def clean_testbed(self):
        try:
            files_in_directory = os.listdir(self.cwd)

            log_files = [
                file for file in files_in_directory if file.endswith(
                    ".log")]

            self.logger.info('Removing log files')

            for log_file in log_files:
                self.remove_file(log_file)

            sh_files = [
                file for file in files_in_directory if file.endswith(
                    ".sh")]

            self.logger.info('Removing shell files')

            for sh_file in sh_files:
                self.remove_file(sh_file)

            yaml_files = [
                file for file in files_in_directory if file.endswith(
                    ".yaml")]

            self.logger.info('Removing yaml files')

            for yaml_file in yaml_files:
                self.remove_file(yaml_file)

            report_files = [
                file for file in files_in_directory if file.endswith(
                    ".html")]

            self.logger.info('Removing report files')

            for report_file in report_files:
                self.remove_file(report_file)

            tar_files = [
                file for file in files_in_directory if file.endswith(
                    ".tar.gz") or file.endswith(".tar")]

            self.logger.info('Removing tar files')

            for tar_file in tar_files:
                self.remove_file(tar_file)

            csv_files = [
                file for file in files_in_directory if file.endswith(
                    ".csv")]

            self.logger.info('Removing csv files')

            for csv_file in csv_files:
                self.remove_file(csv_file)

            py_files = [
                file for file in files_in_directory if file.endswith(
                    ".py")]
            self.logger.info('Removing python files except operator_cicd.py')

            for py_file in py_files:
                if py_file not in ["operator_cicd.py"]:
                    self.remove_file(py_file)

            log_folders = [
                folder for folder in files_in_directory if folder.startswith(
                    "logs")]
            self.logger.info('Removing log folders')

            for log_folder in log_folders:
                self.remove_folder(log_folder)

            kne_folders = [
                folder for folder in files_in_directory if folder.startswith(
                    "kne")]
            self.logger.info('Removing kne folders')

            for kne_folder in kne_folders:
                self.remove_folder(kne_folder)

            csv_folders = [
                folder for folder in files_in_directory if folder.startswith(
                    "parsed")]
            self.logger.info('Removing csv folders')

            for csv_folder in csv_folders:
                self.remove_folder(csv_folder)

            self.logger.info('Removing __pycache__')
            self.remove_folder('__pycache__')

        except Exception as e:
            print(
                "Error in clean_testbed of RunIxiaCOperatorE2E : {}".format(
                    e
                )
            )
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def run_sanity(self):
        try:
            self.deploy_ixia_c_op()
            self.execute_sanity()
            self.cleanup_ixia_c_op()
            self.update_overall_sanity_status()
        except Exception as e:
            print("Error in run_sanity of RunIxiaCOperatorE2E : {}".format(e))
            self.logger.error(traceback.format_exc())
            raise Exception(e)

    def run(self):
        self.get_cmd_line_options()
        if self.test:
            self.get_artifacts()
            self.form_test_metadata()
            self.run_sanity()
        elif self.clean:
            self.clean_testbed()


if __name__ == "__main__":
    run_ixia_c_e2e = RunIxiaCOperatorE2E()
    run_ixia_c_e2e.run()
