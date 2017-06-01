// +build !unit

package mysql

import (
	"fmt"
	"strconv"
	"testing"

	mesos "code.uber.internal/infra/peloton/.gen/mesos/v1"
	"code.uber.internal/infra/peloton/.gen/peloton/api/job"
	"code.uber.internal/infra/peloton/.gen/peloton/api/peloton"
	"code.uber.internal/infra/peloton/.gen/peloton/api/respool"
	"code.uber.internal/infra/peloton/.gen/peloton/api/task"

	"github.com/jmoiron/sqlx"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/suite"
	"github.com/uber-go/tally"
)

const (
	_resPoolOwner = "teamPeloton"
)

type mySQLStoreTestSuite struct {
	suite.Suite
	store *Store
	db    *sqlx.DB
}

func (suite *mySQLStoreTestSuite) SetupSuite() {
	conf := LoadConfigWithDB()

	suite.db = conf.Conn
	suite.store = NewStore(*conf, tally.NoopScope)
}

func (suite *mySQLStoreTestSuite) TearDownSuite() {
	fmt.Println("tearing down")
	//_, err := suite.db.Exec("DELETE FROM task")
	//suite.NoError(err)
}

func TestMysqlStore(t *testing.T) {
	suite.Run(t, new(mySQLStoreTestSuite))
}

func (suite *mySQLStoreTestSuite) TestCreateGetTaskInfo() {
	// Insert 2 jobs
	var nJobs = 3
	var jobIDs []*peloton.JobID
	var jobs []*job.JobConfig
	for i := 0; i < nJobs; i++ {
		var jobID = peloton.JobID{Value: "TestJob_" + strconv.Itoa(i)}
		jobIDs = append(jobIDs, &jobID)
		var sla = job.SlaConfig{
			Priority:                22,
			Preemptible:             false,
			MaximumRunningInstances: 3 + uint32(i),
		}
		var taskConfig = task.TaskConfig{
			Resource: &task.ResourceConfig{
				CpuLimit:    0.8,
				MemLimitMb:  800,
				DiskLimitMb: 1500,
				FdLimit:     1000,
			},
		}
		var jobConfig = job.JobConfig{
			Name:          "TestJob_" + strconv.Itoa(i),
			OwningTeam:    "team6",
			LdapGroups:    []string{"money", "team6", "otto"},
			Sla:           &sla,
			DefaultConfig: &taskConfig,
		}
		jobs = append(jobs, &jobConfig)
		err := suite.store.CreateJob(&jobID, &jobConfig, "uber")
		suite.NoError(err)

		// For each job, create 3 tasks
		for j := uint32(0); j < 3; j++ {
			var tID = fmt.Sprintf("%s-%d", jobID.Value, j)
			var taskInfo = task.TaskInfo{
				Runtime: &task.RuntimeInfo{
					TaskId: &mesos.TaskID{Value: &tID},
					State:  task.TaskState(j),
				},
				Config:     jobConfig.GetDefaultConfig(),
				InstanceId: uint32(j),
				JobId:      &jobID,
			}
			err = suite.store.CreateTask(&jobID, j, &taskInfo, "test")
			suite.NoError(err)
		}
	}
	// List all tasks by job
	for i := 0; i < nJobs; i++ {
		tasks, err := suite.store.GetTasksForJob(jobIDs[i])
		suite.NoError(err)
		suite.Equal(len(tasks), 3)
		for _, task := range tasks {
			suite.Equal(task.JobId.Value, jobIDs[i].Value)
		}
	}

	// Paganate through the job tasks
	for i := 0; i < nJobs; i++ {
		var offset uint32
		for {
			tasks, total, err := suite.store.QueryTasks(jobIDs[i], offset, 1)
			suite.NoError(err)
			if len(tasks) == 0 {
				break
			}
			suite.Equal(uint32(3), total)
			suite.Equal(1, len(tasks))
			suite.Equal(jobIDs[i].Value, tasks[0].JobId.Value)
			offset++
		}
		suite.Equal(uint32(3), offset)
	}

	// List tasks for a job in certain state
	// TODO: change the task.runtime.State to string type

	// Update task
	// List all tasks by job
	for i := 0; i < nJobs; i++ {
		tasks, err := suite.store.GetTasksForJob(jobIDs[i])
		suite.NoError(err)
		suite.Equal(len(tasks), 3)
		for _, task := range tasks {
			task.Runtime.Host = fmt.Sprintf("compute-%d", i)
			err := suite.store.UpdateTask(task)
			suite.NoError(err)
		}
	}
	for i := 0; i < nJobs; i++ {
		tasks, err := suite.store.GetTasksForJob(jobIDs[i])
		suite.NoError(err)
		suite.Equal(len(tasks), 3)
		for _, task := range tasks {
			suite.Equal(task.Runtime.Host, fmt.Sprintf("compute-%d", i))
		}
	}
}

// TestCreateTasks ensures mysql task create batching works as expected.
func (suite *mySQLStoreTestSuite) TestCreateTasks() {
	jobTasks := map[string]int{
		"TestJob1": 10,
		"TestJob2": suite.store.Conf.MaxBatchSize,
		"TestJob3": suite.store.Conf.MaxBatchSize * 3,
		"TestJob4": suite.store.Conf.MaxBatchSize*3 + 10,
	}
	for jobID, nTasks := range jobTasks {
		var jobID = peloton.JobID{Value: jobID}
		var sla = job.SlaConfig{
			Priority:                22,
			Preemptible:             false,
			MaximumRunningInstances: 3,
		}
		var taskConfig = task.TaskConfig{
			Resource: &task.ResourceConfig{
				CpuLimit:    0.8,
				MemLimitMb:  800,
				DiskLimitMb: 1500,
				FdLimit:     1000,
			},
		}
		var jobConfig = job.JobConfig{
			Name:          jobID.Value,
			OwningTeam:    "team6",
			LdapGroups:    []string{"money", "team6", "otto"},
			Sla:           &sla,
			DefaultConfig: &taskConfig,
		}
		err := suite.store.CreateJob(&jobID, &jobConfig, "uber")
		suite.NoError(err)

		// now, create a mess of tasks
		taskInfos := []*task.TaskInfo{}
		for j := 0; j < nTasks; j++ {
			var tID = fmt.Sprintf("%s-%d", jobID.Value, j)
			var taskInfo = task.TaskInfo{
				Runtime: &task.RuntimeInfo{
					TaskId: &mesos.TaskID{Value: &tID},
					State:  task.TaskState(j),
				},
				Config:     jobConfig.GetDefaultConfig(),
				InstanceId: uint32(j),
				JobId:      &jobID,
			}
			taskInfos = append(taskInfos, &taskInfo)
		}
		err = suite.store.CreateTasks(&jobID, taskInfos, "test")
		suite.NoError(err)
	}

	// List all tasks by job, ensure they were created properly, and have the right parent
	for jobID, nTasks := range jobTasks {
		job := peloton.JobID{Value: jobID}
		tasks, err := suite.store.GetTasksForJob(&job)
		suite.NoError(err)
		suite.Equal(nTasks, len(tasks))
		for _, task := range tasks {
			suite.Equal(jobID, task.JobId.Value)
		}
	}
}

func (suite *mySQLStoreTestSuite) TestCreateGetJobConfig() {
	// Create 10 jobs in db
	var originalJobs []*job.JobConfig
	var records = 10
	var keys = []string{"testKey0", "testKey1", "testKey2", "key0"}
	var vals = []string{"testVal0", "testVal1", "testVal2", "val0"}
	for i := 0; i < records; i++ {
		var jobID = peloton.JobID{Value: "TestJob_" + strconv.Itoa(i)}
		var sla = job.SlaConfig{
			Priority:                22,
			Preemptible:             false,
			MaximumRunningInstances: 3,
		}
		var taskConfig = task.TaskConfig{
			Resource: &task.ResourceConfig{
				CpuLimit:    0.8,
				MemLimitMb:  800,
				DiskLimitMb: 1500,
				FdLimit:     1000,
			},
		}
		var labels = mesos.Labels{
			Labels: []*mesos.Label{
				{Key: &keys[0], Value: &vals[0]},
				{Key: &keys[1], Value: &vals[1]},
				{Key: &keys[2], Value: &vals[2]},
			},
		}
		// Add owner to job0 and job1
		var owner = "team6"
		if i < 2 {
			owner = "money"
		}

		// Add some special label to job0 and job1
		if i < 2 {
			labels.Labels = append(labels.Labels,
				&mesos.Label{Key: &keys[3], Value: &vals[3]})
		}

		var jobconfig = job.JobConfig{
			Name:          "TestJob_" + strconv.Itoa(i),
			Sla:           &sla,
			OwningTeam:    owner,
			LdapGroups:    []string{"money", "team6", "otto"},
			DefaultConfig: &taskConfig,
			Labels:        &labels,
		}
		originalJobs = append(originalJobs, &jobconfig)
		err := suite.store.CreateJob(&jobID, &jobconfig, "uber")
		suite.NoError(err)

		err = suite.store.CreateJob(&jobID, &jobconfig, "uber")
		suite.Error(err)
	}
	// search by job ID
	for i := 0; i < records; i++ {
		var jobID = peloton.JobID{Value: "TestJob_" + strconv.Itoa(i)}
		result, err := suite.store.GetJobConfig(&jobID)
		suite.NoError(err)
		suite.Equal(result.Name, originalJobs[i].Name)
		suite.Equal(result.DefaultConfig.Resource.FdLimit,
			originalJobs[i].DefaultConfig.Resource.FdLimit)
		suite.Equal(result.Sla.MaximumRunningInstances,
			originalJobs[i].Sla.MaximumRunningInstances)
	}

	// Query by owner
	var jobs map[string]*job.JobConfig
	var err error
	var labelQuery = mesos.Labels{
		Labels: []*mesos.Label{
			{Key: &keys[0], Value: &vals[0]},
			{Key: &keys[1], Value: &vals[1]},
		},
	}
	jobs, err = suite.store.Query(&labelQuery, nil)
	suite.NoError(err)
	suite.Equal(len(jobs), len(originalJobs))

	labelQuery = mesos.Labels{
		Labels: []*mesos.Label{
			{Key: &keys[0], Value: &vals[0]},
			{Key: &keys[1], Value: &vals[1]},
			{Key: &keys[3], Value: &vals[3]},
		},
	}
	jobs, err = suite.store.Query(&labelQuery, nil)
	suite.NoError(err)
	suite.Equal(len(jobs), 2)
	labelQuery = mesos.Labels{
		Labels: []*mesos.Label{
			{Key: &keys[3], Value: &vals[3]},
		},
	}
	jobs, err = suite.store.Query(&labelQuery, nil)
	suite.NoError(err)
	suite.Equal(len(jobs), 2)

	labelQuery = mesos.Labels{
		Labels: []*mesos.Label{
			{Key: &keys[2], Value: &vals[3]},
		},
	}
	jobs, err = suite.store.Query(&labelQuery, nil)
	suite.NoError(err)
	suite.Equal(len(jobs), 0)

	labelQuery = mesos.Labels{
		Labels: []*mesos.Label{},
	}
	// test get all jobs if no labels
	jobs, err = suite.store.Query(&labelQuery, nil)
	suite.NoError(err)
	suite.Equal(len(jobs), 10)

	jobs, err = suite.store.Query(nil, nil)
	suite.NoError(err)
	suite.Equal(len(jobs), 10)

	jobs, err = suite.store.GetJobsByOwner("team6")
	suite.NoError(err)
	suite.Equal(len(jobs), records-2)

	jobs, err = suite.store.GetJobsByOwner("money")
	suite.NoError(err)
	suite.Equal(len(jobs), 2)

	jobs, err = suite.store.GetJobsByOwner("owner")
	suite.NoError(err)
	suite.Equal(len(jobs), 0)

	// Delete job
	for i := 0; i < records; i++ {
		var jobID = peloton.JobID{Value: "TestJob_" + strconv.Itoa(i)}
		err := suite.store.DeleteJob(&jobID)
		suite.NoError(err)
	}
	// Get should not return anything
	for i := 0; i < records; i++ {
		var jobID = peloton.JobID{Value: "TestJob_" + strconv.Itoa(i)}
		result, err := suite.store.GetJobConfig(&jobID)
		suite.NotNil(err)
		suite.Nil(result)
	}
}

func (suite *mySQLStoreTestSuite) TestUpdateJobConfig() {
	oldInstanceCount := uint32(6)
	newInstanceCount := uint32(10)
	jobConfig := createJobConfig()
	var jobID = peloton.JobID{Value: uuid.New()}
	err := suite.store.CreateJob(&jobID, jobConfig, "test")
	suite.NoError(err)
	jobConfig, err = suite.store.GetJobConfig(&jobID)

	suite.Equal(oldInstanceCount, jobConfig.InstanceCount)

	jobConfig.InstanceCount = newInstanceCount
	err = suite.store.UpdateJobConfig(&jobID, jobConfig)
	suite.NoError(err)
	suite.Equal(newInstanceCount, jobConfig.InstanceCount)
}

func (suite *mySQLStoreTestSuite) TestGetResourcePoolsByOwner() {
	nResourcePools := 2

	// todo move to setup once ^^^ issue resolves
	for i := 0; i < nResourcePools; i++ {
		resourcePoolID := &respool.ResourcePoolID{Value: fmt.Sprintf("%s%d", _resPoolOwner, i)}
		resourcePoolConfig := createResourcePoolConfig()
		resourcePoolConfig.Name = resourcePoolID.Value
		err := suite.store.CreateResourcePool(resourcePoolID, resourcePoolConfig, _resPoolOwner)
		suite.Nil(err)
	}

	testCases := []struct {
		expectedErr    error
		owner          string
		nResourcePools int
		msg            string
	}{
		{
			expectedErr:    nil,
			owner:          "idon'texist",
			nResourcePools: 0,
			msg:            "testcase: fetch resource pools by non existent owner",
		},
		{
			expectedErr:    nil,
			owner:          _resPoolOwner,
			nResourcePools: nResourcePools,
			msg:            "testcase: fetch resource pools owner",
		},
	}

	for _, tc := range testCases {
		resourcePools, actualErr := suite.store.GetResourcePoolsByOwner(tc.owner)
		if tc.expectedErr == nil {
			suite.Nil(actualErr, tc.msg)
			suite.Len(resourcePools, tc.nResourcePools, tc.msg)
		} else {
			suite.EqualError(actualErr, tc.expectedErr.Error(), tc.msg)
		}
	}
}

func (suite *mySQLStoreTestSuite) TestJobRuntime() {
	var jobStore = suite.store
	nTasks := 20

	// CreateJob should create the default job runtime
	var jobID = peloton.JobID{Value: "TestJobRuntime"}
	jobConfig := createJobConfig()
	jobConfig.InstanceCount = uint32(nTasks)
	err := jobStore.CreateJob(&jobID, jobConfig, "uber")
	suite.NoError(err)

	runtime, err := jobStore.GetJobRuntime(&jobID)
	suite.NoError(err)
	suite.Equal(job.JobState_INITIALIZED, runtime.State)
	suite.Equal(0, len(runtime.TaskStats))

	// update job runtime
	runtime.State = job.JobState_RUNNING
	runtime.TaskStats[task.TaskState_PENDING.String()] = 5
	runtime.TaskStats[task.TaskState_PLACED.String()] = 5
	runtime.TaskStats[task.TaskState_RUNNING.String()] = 5
	runtime.TaskStats[task.TaskState_SUCCEEDED.String()] = 5

	err = jobStore.UpdateJobRuntime(&jobID, runtime)
	suite.NoError(err)

	runtime, err = jobStore.GetJobRuntime(&jobID)
	suite.NoError(err)
	suite.Equal(job.JobState_RUNNING, runtime.State)
	suite.Equal(4, len(runtime.TaskStats))

	jobIds, err := jobStore.GetJobsByState(job.JobState_RUNNING)
	suite.NoError(err)
	idFound := false
	for _, id := range jobIds {
		if id.Value == jobID.Value {
			idFound = true
		}
	}
	suite.True(idFound)

	jobIds, err = jobStore.GetJobsByState(120)
	suite.NoError(err)
	suite.Equal(0, len(jobIds))
}

// Returns mock resource pool config
func createResourcePoolConfig() *respool.ResourcePoolConfig {
	return &respool.ResourcePoolConfig{
		Name:        "TestResourcePool_1",
		ChangeLog:   nil,
		Description: "test resource pool",
		LdapGroups:  []string{"l1", "l2"},
		OwningTeam:  "team1",
		Parent:      nil,
		Policy:      1,
		Resources:   createResourceConfigs(),
	}
}

// Returns mock list of resource configs
func createResourceConfigs() []*respool.ResourceConfig {
	return []*respool.ResourceConfig{
		{
			Kind:        "cpu",
			Limit:       1000.0,
			Reservation: 100.0,
			Share:       1.0,
		},
		{
			Kind:        "gpu",
			Limit:       4.0,
			Reservation: 2.0,
			Share:       1.0,
		},
	}
}

func createJobConfig() *job.JobConfig {
	var sla = job.SlaConfig{
		Priority:                22,
		MaximumRunningInstances: 6,
		Preemptible:             false,
	}
	var jobConfig = job.JobConfig{
		OwningTeam:    "uber",
		LdapGroups:    []string{"team1", "team2", "team3"},
		Sla:           &sla,
		InstanceCount: uint32(6),
	}
	return &jobConfig
}
