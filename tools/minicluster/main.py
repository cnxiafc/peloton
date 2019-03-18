#!/usr/bin/env python
"""
 -- Locally run and manage a personal cluster in containers.

This script can be used to manage (setup, teardown) a personal
Mesos cluster etc in containers, optionally Peloton
master or apps can be specified to run in containers as well.

@copyright:  2019 Uber Compute Platform. All rights reserved.

@license:    license

@contact:    peloton-dev@uber.com
"""

from argparse import ArgumentParser
from argparse import RawDescriptionHelpFormatter
from collections import OrderedDict

import minicluster
import print_utils

__date__ = '2016-12-08'
__author__ = 'wu'

RESOURCE_MANAGER = 1
HOST_MANAGER = 2
PLACEMENT_ENGINE = 3
JOB_MANAGER = 4
ARCHIVER = 5
AURORABRIDGE = 6

# Defines the order in which the apps are started
# NB: HOST_MANAGER is tied to database migrations so should be started first
APP_START_ORDER = OrderedDict([
    (HOST_MANAGER, minicluster.run_peloton_hostmgr),
    (RESOURCE_MANAGER, minicluster.run_peloton_resmgr),
    (PLACEMENT_ENGINE, minicluster.run_peloton_placement),
    (JOB_MANAGER, minicluster.run_peloton_jobmgr),
    (ARCHIVER, minicluster.run_peloton_archiver),
    (AURORABRIDGE, minicluster.run_peloton_aurorabridge)]
)


#
# Run peloton
#
def run_peloton(applications):
    print_utils.okblue('docker image "uber/peloton" has to be built first '
                       'locally by running IMAGE=uber/peloton make docker')

    for app, func in APP_START_ORDER.iteritems():
        if app in applications:
            should_disable = applications[app]
            if should_disable:
                continue
        APP_START_ORDER[app]()


#
# Set up a personal cluster
#
def setup(disable_mesos=False, applications={}, enable_peloton=False):
    minicluster.run_cassandra()
    if not disable_mesos:
        minicluster.run_mesos()

    if enable_peloton:
        run_peloton(applications)


def parse_arguments():
    program_shortdesc = __import__('__main__').__doc__.split("\n")[1]
    program_license = '''%s

  Created by %s on %s.
  Copyright Uber Compute Platform. All rights reserved.

USAGE
''' % (program_shortdesc, __author__, str(__date__))
    # Setup argument parser
    parser = ArgumentParser(
        description=program_license,
        formatter_class=RawDescriptionHelpFormatter)
    subparsers = parser.add_subparsers(help='command help', dest='command')
    # Subparser for the 'setup' command
    parser_setup = subparsers.add_parser(
        'setup',
        help='set up a personal cluster')
    parser_setup.add_argument(
        "--no-mesos",
        dest="disable_mesos",
        action='store_true',
        default=False,
        help="disable mesos setup"
    )
    parser_setup.add_argument(
        "--zk_url",
        dest="zk_url",
        action='store',
        type=str,
        default=None,
        help="zk URL when pointing to a pre-existing zk"
    )
    parser_setup.add_argument(
        "-a",
        "--enable-peloton",
        dest="enable_peloton",
        action='store_true',
        default=False,
        help="enable peloton",
    )
    parser_setup.add_argument(
        "--no-resmgr",
        dest="disable_peloton_resmgr",
        action='store_true',
        default=False,
        help="disable peloton resmgr app"
    )
    parser_setup.add_argument(
        "--no-hostmgr",
        dest="disable_peloton_hostmgr",
        action='store_true',
        default=False,
        help="disable peloton hostmgr app"
    )
    parser_setup.add_argument(
        "--no-jobmgr",
        dest="disable_peloton_jobmgr",
        action='store_true',
        default=False,
        help="disable peloton jobmgr app"
    )
    parser_setup.add_argument(
        "--no-placement",
        dest="disable_peloton_placement",
        action='store_true',
        default=False,
        help="disable peloton placement engine app"
    )
    parser_setup.add_argument(
        "--no-archiver",
        dest="disable_peloton_archiver",
        action='store_true',
        default=False,
        help="disable peloton archiver app"
    )
    parser_setup.add_argument(
        "--no-aurorabridge",
        dest="disable_peloton_aurorabridge",
        action='store_true',
        default=False,
        help="disable peloton aurora bridge app"
    )
    # Subparser for the 'teardown' command
    subparsers.add_parser('teardown', help='tear down a personal cluster')
    # Process arguments
    return parser.parse_args()


def main():
    args = parse_arguments()

    command = args.command

    if command == 'setup':
        applications = {
            HOST_MANAGER: args.disable_peloton_hostmgr,
            RESOURCE_MANAGER: args.disable_peloton_resmgr,
            PLACEMENT_ENGINE: args.disable_peloton_placement,
            JOB_MANAGER: args.disable_peloton_jobmgr,
            ARCHIVER: args.disable_peloton_archiver,
            AURORABRIDGE: args.disable_peloton_aurorabridge
        }

        global zk_url
        zk_url = args.zk_url
        setup(
            disable_mesos=args.disable_mesos,
            enable_peloton=args.enable_peloton,
            applications=applications
        )
    elif command == 'teardown':
        minicluster.teardown()
    else:
        # Should never get here.  argparser should prevent it.
        print_utils.fail('Unknown command: %s' % command)
        return 1


if __name__ == "__main__":
    main()
