import logging
import grpc
import time

from client import Client
from pool import Pool
from pod import Pod
from common import IntegrationTestConfig, wait_for_condition
from util import load_test_config

from google.protobuf import json_format

from peloton_client.pbgen.peloton.api.v1alpha import peloton_pb2 as v1alpha_peloton
from peloton_client.pbgen.peloton.api.v1alpha.job.stateless import \
    stateless_pb2 as stateless
from peloton_client.pbgen.peloton.api.v1alpha.job.stateless.svc import \
    stateless_svc_pb2 as stateless_svc
from peloton_client.pbgen.peloton.api.v1alpha.pod.svc import pod_svc_pb2 as pod_svc
from peloton_client.pbgen.peloton.api.v1alpha.pod import pod_pb2 as pod

log = logging.getLogger(__name__)

INVALID_ENTITY_VERSION_ERR_MESSAGE = 'unexpected entity version'


class StatelessJob(object):
    """
    Job represents a peloton stateless job
    """

    def __init__(self, job_file='test_stateless_job_spec.yaml',
                 client=None,
                 config=None,
                 pool=None,
                 job_config=None):

        self.config = config or IntegrationTestConfig()
        self.client = client or Client()
        self.pool = pool or Pool(self.config, self.client)
        self.job_id = None
        self.entity_version = None
        if job_config is None:
            job_spec_dump = load_test_config(job_file)
            job_spec = stateless.JobSpec()
            json_format.ParseDict(job_spec_dump, job_spec)
        self.job_spec = job_spec

    def create(self):
        """
        creates a job based on the config
        :return: the job ID
        """
        respool_id = self.pool.ensure_exists()

        self.job_spec.respool_id.value = respool_id
        request = stateless_svc.CreateJobRequest(
            spec=self.job_spec,
        )
        resp = self.client.stateless_svc.CreateJob(
            request,
            metadata=self.client.jobmgr_metadata,
            timeout=self.config.rpc_timeout_sec,
        )
        assert resp.job_id.value
        self.job_id = resp.job_id.value
        self.entity_version = resp.version.value
        log.info('created job %s with entity version %s',
                 self.job_id, self.entity_version)

    def start(self, ranges=None, entity_version=None):
        """
        Starts certain pods based on the ranges.
        If ranges is not provided it starts all pods of the job

        Job level start does not support range.
        We are using pod api for range operation.
        We do this for backward compatibility of existing tests

        :param ranges: the instance ranges to start
        :param entity_version: the entity version of the job, for concurrency control.
            If entity_version is provided, start will use the provided value,
            and raise an exception if version is wrong.
            if entity_version is not provided, start will query job runtime to
            get config version and retry until version is correct.
        :return: start response from the API
        """
        if ranges is None:
            job_entity_version = entity_version or \
                self.entity_version or \
                self.get_status().version.value

            while True:
                request = stateless_svc.StartJobRequest(
                    job_id=v1alpha_peloton.JobID(value=self.job_id),
                    version=v1alpha_peloton.EntityVersion(value=job_entity_version),
                )
                try:
                    resp = self.client.stateless_svc.StartJob(
                        request,
                        metadata=self.client.jobmgr_metadata,
                        timeout=self.config.rpc_timeout_sec,
                    )
                except grpc.RpcError as e:
                    # if entity version is incorrect, get entity version from job status
                    # and try again.
                    if e.code() == grpc.StatusCode.INVALID_ARGUMENT \
                            and e.details() == INVALID_ENTITY_VERSION_ERR_MESSAGE \
                            and entity_version is None:
                        job_entity_version = entity_version or \
                            self.get_status().version.value
                        continue
                    raise
                break
            self.entity_version = resp.version.value
            log.info('job started, new entity version: %s', self.entity_version)
            return resp

        for pod_range in ranges:
            for pod_id in range(getattr(pod_range, 'from'), pod_range.to):
                pod_name = self.job_id + '-' + str(pod_id)
                request = pod_svc.StartPodRequest(
                    pod_name=v1alpha_peloton.PodName(value=pod_name),
                )
                self.client.pod_svc.StartPod(
                    request,
                    metadata=self.client.jobmgr_metadata,
                    timeout=self.config.rpc_timeout_sec,
                )

        log.info('starting pods in job {0} with ranges {1}'
                 .format(self.job_id, ranges))
        return pod_svc.StartPodResponse()

    def stop(self, ranges=None, entity_version=None):
        """
        Stops certain pods based on the ranges.
        If ranges is not provided then it stops the job

        Job level stop does not support range.
        We are using pod api for range operation.
        We do this for backward compatibility of existing tests

        :param ranges: the instance ranges to stop
        :param entity_version: the entity version of the job, for concurrency control.
            If entity_version is provided, stop will use the provided value,
            and raise an exception if version is wrong.
            if entity_version is not provided, stop will query job runtime to
            get config version and retry until version is correct.
        :return: stop response from the API
        """
        if ranges is None:
            job_entity_version = entity_version or \
                self.entity_version or \
                self.get_status().version.value

            while True:
                request = stateless_svc.StopJobRequest(
                    job_id=v1alpha_peloton.JobID(value=self.job_id),
                    version=v1alpha_peloton.EntityVersion(value=job_entity_version),
                )
                try:
                    resp = self.client.stateless_svc.StopJob(
                        request,
                        metadata=self.client.jobmgr_metadata,
                        timeout=self.config.rpc_timeout_sec,
                    )
                except grpc.RpcError as e:
                    # if entity version is incorrect, get entity version from job status
                    # and try again.
                    if e.code() == grpc.StatusCode.INVALID_ARGUMENT \
                            and e.details() == INVALID_ENTITY_VERSION_ERR_MESSAGE \
                            and entity_version is None:
                        job_entity_version = entity_version or \
                            self.get_status().version.value
                        continue
                    raise
                break
            self.entity_version = resp.version.value
            log.info('job stopped, new entity version: %s', self.entity_version)
            return resp

        for pod_range in ranges:
            for pod_id in range(getattr(pod_range, 'from'), pod_range.to):
                pod_name = self.job_id + '-' + str(pod_id)
                request = pod_svc.StopPodRequest(
                    pod_name=v1alpha_peloton.PodName(value=pod_name),
                )
                self.client.pod_svc.StopPod(
                    request,
                    metadata=self.client.jobmgr_metadata,
                    timeout=self.config.rpc_timeout_sec,
                )

        log.info('stopping pods in job {0} with ranges {1}'
                 .format(self.job_id, ranges))
        return pod_svc.StopPodResponse()

    def restart(self, entity_version=None, batch_size=None, ranges=None):
        """
        Restart pods based on the ranges.
        If ranges is not provided then it restarts all pods of the job

        :return: restart response from the API
        """
        job_entity_version = entity_version or \
            self.entity_version or \
            self.get_status().version.value

        while True:
            request = stateless_svc.RestartJobRequest(
                job_id=v1alpha_peloton.JobID(value=self.job_id),
                version=v1alpha_peloton.EntityVersion(value=job_entity_version),
                batch_size=batch_size,
                ranges=ranges,
            )
            try:
                resp = self.client.stateless_svc.RestartJob(
                    request,
                    metadata=self.client.jobmgr_metadata,
                    timeout=self.config.rpc_timeout_sec,
                )
            except grpc.RpcError as e:
                # if entity version is incorrect, get entity version from job status
                # and try again.
                if e.code() == grpc.StatusCode.INVALID_ARGUMENT \
                        and e.details() == INVALID_ENTITY_VERSION_ERR_MESSAGE \
                        and entity_version is None:
                    job_entity_version = entity_version or \
                        self.get_status().version.value
                    continue
                raise
            break
        self.entity_version = resp.version.value
        log.info('job restarted, new entity version: %s', self.entity_version)
        return resp

    def wait_for_state(self, goal_state='SUCCEEDED', failed_state='FAILED'):
        """
        Waits for the job to reach a particular state
        :param goal_state: The state to reach
        :param failed_state: The failed state of the job
        """
        state = ''
        attempts = 0
        start = time.time()
        log.info('%s waiting for state %s', self.job_id, goal_state)
        state_transition_failure = False
        # convert the name from v0 state name to v1 alpha state name,
        # so the function signature can be shared between the apis
        goal_state = 'JOB_STATE_' + goal_state
        failed_state = 'JOB_STATE_' + failed_state
        while attempts < self.config.max_retry_attempts:
            try:
                request = stateless_svc.GetJobRequest(
                    job_id=v1alpha_peloton.JobID(value=self.job_id),
                )
                resp = self.client.stateless_svc.GetJob(
                    request,
                    metadata=self.client.jobmgr_metadata,
                    timeout=self.config.rpc_timeout_sec,
                )
                status = resp.job_info.status
                new_state = stateless.JobState.Name(status.state)
                if state != new_state:
                    log.info('%s transitioned to state %s', self.job_id,
                             new_state)
                state = new_state
                if state == goal_state:
                    break
                # If we assert here, we will log the exception,
                # and continue with the finally block. Set a flag
                # here to indicate failure and then break the loop
                # in the finally block
                if state == failed_state:
                    state_transition_failure = True
            except Exception as e:
                log.warn(e)
            finally:
                if state_transition_failure:
                    break
                time.sleep(self.config.sleep_time_sec)
                attempts += 1

        if state_transition_failure:
            log.info('goal_state:%s current_state:%s attempts: %s',
                     goal_state, state, str(attempts))
            assert False

        if attempts == self.config.max_retry_attempts:
            log.info('%s max attempts reached to wait for goal state',
                     self.job_id)
            log.info('goal_state:%s current_state:%s', goal_state, state)
            assert False

        end = time.time()
        elapsed = end - start
        log.info('%s state transition took %s seconds', self.job_id, elapsed)
        assert state == goal_state

    def wait_for_condition(self, condition):
        """
        Waits for a particular condition to be met with the job
        :param condition: The condition to meet
        """
        wait_for_condition(message=self.job_id, condition=condition, config=self.config)

    def get_task(self, instance_id):
        """
        name it as get_task for compatibility with batch job, so
        some tests can be shared
        :param instance_id: The instance id of the task
        :return: The Task of the job based on the instance id
        """
        return self.get_pod(instance_id)

    def get_pod(self, pod_id):
        """
        :param pod_id: The pod id of the pod
        :return: The Pod of the job based on the instance id
        """
        return Pod(self, pod_id)

    def get_pod_status(self, instance_id):
        """
        Get status of a pod
        :param instance_id: id of the pod
        """
        request = pod_svc.GetPodRequest(
            pod_name=v1alpha_peloton.PodName(value=self.job_id + '-' + str(instance_id)),
            status_only=True,
        )

        resp = self.client.pod_svc.GetPod(
            request,
            metadata=self.client.jobmgr_metadata,
            timeout=self.config.rpc_timeout_sec,
        )

        return resp.current.status

    def get_job(self):
        """
        :return: the configuration and runtime status of a job.
        """
        request = stateless_svc.GetJobRequest(
            job_id=v1alpha_peloton.JobID(value=self.job_id),
        )
        resp = self.client.stateless_svc.GetJob(
            request,
            metadata=self.client.jobmgr_metadata,
            timeout=self.config.rpc_timeout_sec,
        )
        return resp

    def get_info(self):
        """
        :return: info of a job.
        """
        return self.get_job().job_info

    def get_status(self):
        """
        :return: status of a job.
        """
        return self.get_info().status

    def get_spec(self):
        """
        :return: spec of a job.
        """
        return self.get_info().spec

    def wait_for_all_pods_running(self):
        """
        Waits for all pods in the job in RUNNING state
        """
        attempts = 0
        start = time.time()
        while attempts < self.config.max_retry_attempts:
            try:
                count = 0
                for pod_id in range(0, self.job_spec.instance_count):
                    pod_state = self.get_pod(pod_id).get_pod_status().state
                    if pod_state == pod.POD_STATE_RUNNING:
                        count += 1

                if count == self.job_spec.instance_count:
                    log.info('%s job has %s running pods', self.job_id, count)
                    break
            except Exception as e:
                log.warn(e)

            time.sleep(self.config.sleep_time_sec)
            attempts += 1

        if attempts == self.config.max_retry_attempts:
            log.info('max attempts reached to wait for all tasks running')
            assert False

        end = time.time()
        elapsed = end - start
        log.info('%s job has all running pods in %s seconds', self.job_id, elapsed)

    def wait_for_terminated(self):
        """
        Waits for the job to be terminated
        """
        state = ''
        attempts = 0
        log.info('%s waiting for terminal state', self.job_id)
        terminated = False
        while attempts < self.config.max_retry_attempts:
            try:
                status = self.get_status()
                new_state = stateless.JobState.Name(status.state)
                if state != new_state:
                    log.info('%s transitioned to state %s', self.job_id,
                             new_state)
                state = new_state
                if state in ['JOB_STATE_SUCCEEDED',
                             'JOB_STATE_FAILED',
                             'JOB_STATE_KILLED']:
                    terminated = True
                    break
            except Exception as e:
                log.warn(e)
            finally:
                time.sleep(self.config.sleep_time_sec)
                attempts += 1
        if terminated:
            log.info('%s job terminated', self.job_id)
            assert True

        if attempts == self.config.max_retry_attempts:
            log.info('%s max attempts reached to wait for goal state',
                     self.job_id)
            log.info('current_state:%s', state)
            assert False

    def wait_for_workflow_state(self, goal_state='SUCCEEDED', failed_state='FAILED'):
        """
        Waits for the job workflow to reach a particular state
        :param goal_state: The state to reach
        :param failed_state: The failed state of the job
        """
        state = ''
        attempts = 0
        start = time.time()
        log.info('%s waiting for state workflow %s', self.job_id, goal_state)
        state_transition_failure = False
        # convert the name from v0 state name to v1 alpha state name,
        # so the function signature can be shared between the apis
        goal_state = 'WORKFLOW_STATE_' + goal_state
        failed_state = 'WORKFLOW_STATE_' + failed_state
        while attempts < self.config.max_retry_attempts:
            try:
                request = stateless_svc.GetJobRequest(
                    job_id=v1alpha_peloton.JobID(value=self.job_id),
                )
                resp = self.client.stateless_svc.GetJob(
                    request,
                    metadata=self.client.jobmgr_metadata,
                    timeout=self.config.rpc_timeout_sec,
                )
                status = resp.workflow_info.status
                new_state = stateless.WorkflowState.Name(status.state)
                if state != new_state:
                    log.info('%s transitioned to state %s', self.job_id,
                             new_state)
                state = new_state
                if state == goal_state:
                    break
                # If we assert here, we will log the exception,
                # and continue with the finally block. Set a flag
                # here to indicate failure and then break the loop
                # in the finally block
                if state == failed_state:
                    state_transition_failure = True
            except Exception as e:
                log.warn(e)
            finally:
                if state_transition_failure:
                    break
                time.sleep(self.config.sleep_time_sec)
                attempts += 1

        if state_transition_failure:
            log.info('goal_state:%s current_state:%s attempts: %s',
                     goal_state, state, str(attempts))
            assert False

        if attempts == self.config.max_retry_attempts:
            log.info('%s max attempts reached to wait for goal state',
                     self.job_id)
            log.info('goal_state:%s current_state:%s', goal_state, state)
            assert False

        end = time.time()
        elapsed = end - start
        log.info('%s state transition took %s seconds', self.job_id, elapsed)

    def query_pods(self):
        """
        :return: list of pod info of all matching pod
        """
        request = stateless_svc.QueryPodsRequest(
            job_id=v1alpha_peloton.JobID(value=self.job_id),
        )
        resp = self.client.stateless_svc.QueryPods(
            request,
            metadata=self.client.jobmgr_metadata,
            timeout=self.config.rpc_timeout_sec,
        )
        return resp.pods

    def delete(self, entity_version=None, force_delete=False):
        """
        Delete the job

        :param entity_version: the entity version of the job, for concurrency control.
            If entity_version is provided,  start will use the provided value,
            and raise an exception if version is wrong.
            if entity_version is not provided, start will query job runtime to
            get config version and retry until version is correct.
        :param force_delete: force delete a job.  If set to true, it will force
            a delete of the job even if it is running.The job will be first
            stopped and deleted. This step cannot be undone, and the job cannot
            be re-created (with same uuid) till the delete is complete.
        """
        job_entity_version = entity_version or \
            self.entity_version or \
            self.get_status().version.value

        while True:
            request = stateless_svc.DeleteJobRequest(
                job_id=v1alpha_peloton.JobID(value=self.job_id),
                version=v1alpha_peloton.EntityVersion(value=job_entity_version),
                force=force_delete,
            )
            try:
                self.client.stateless_svc.DeleteJob(
                    request,
                    metadata=self.client.jobmgr_metadata,
                    timeout=self.config.rpc_timeout_sec,
                )
            except grpc.RpcError as e:
                # if entity version is incorrect, get entity version from job status
                # and try again.
                if e.code() == grpc.StatusCode.INVALID_ARGUMENT \
                        and e.details() == INVALID_ENTITY_VERSION_ERR_MESSAGE \
                        and entity_version is None:
                    job_entity_version = entity_version or \
                        self.get_status().version.value
                    continue
                raise
            break
        log.info('job %s deleted', self.job_id)

    def get_pods(self):
        """
        :return: All the pods of the job
        """
        return {Pod(self, iid) for iid in xrange(self.job_spec.instance_count)}
