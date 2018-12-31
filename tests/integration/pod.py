from peloton_client.pbgen.peloton.api.v1alpha.pod import pod_pb2 as pod
from peloton_client.pbgen.peloton.api.v1alpha.pod.svc import pod_svc_pb2 as pod_svc
from peloton_client.pbgen.peloton.api.v1alpha import peloton_pb2 as v1alpha_peloton

POD_STATE_PREFIX = 'POD_STATE_'


class Pod(object):
    """
    Pod represents a peloton pod for stateless job
    """

    def __init__(self, job, instance_id):
        self.stateless_job = job
        self.instance_id = instance_id

    def get_pod_status(self):
        return self.stateless_job.get_pod_status(self.instance_id)

    def get_pod_events(self):
        pod_name = self.stateless_job.job_id + '-' + str(self.instance_id)
        request = pod_svc.GetPodEventsRequest(
            pod_name=v1alpha_peloton.PodName(value=pod_name)
        )
        resp = self.stateless_job.client.pod_svc.GetPodEvents(
            request,
            metadata=self.stateless_job.client.jobmgr_metadata,
            timeout=self.stateless_job.config.rpc_timeout_sec,
        )
        return resp.events

    @property
    def state_str(self):
        state_name = pod.PodState.Name(self.get_pod_status().state)
        # trim the prefix so pod state is the same as old task state,
        # and the api can be used for both job tests
        return state_name[len(POD_STATE_PREFIX):]