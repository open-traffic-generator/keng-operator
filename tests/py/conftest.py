import pytest


def pytest_addoption(parser):
    # called before running tests to register command line options for pytest
    parser.addoption("--ixia_c_release",
                     type=str,
                     action="store",
                     default="0.0.1-2185")


@pytest.fixture(scope='session')
def ixia_c_release(pytestconfig):
    ixia_c_release = pytestconfig.getoption("--ixia_c_release")
    return ixia_c_release