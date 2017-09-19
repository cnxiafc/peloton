package goalstate

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"

	"code.uber.internal/infra/peloton/.gen/peloton/api/task"
	"code.uber.internal/infra/peloton/jobmgr/tracked"
	"code.uber.internal/infra/peloton/util"
)

var (
	// _isoVersionsTaskRules maps current states to action, given a goal state:
	// goal-state -> current-state -> action.
	// It assumes task's runtime and goal are at the same version
	_isoVersionsTaskRules = map[task.TaskState]map[task.TaskState]tracked.TaskAction{
		task.TaskState_RUNNING: {
			task.TaskState_INITIALIZED: tracked.StartAction,
		},
		task.TaskState_SUCCEEDED: {
			task.TaskState_INITIALIZED: tracked.StartAction,
			task.TaskState_SUCCEEDED:   tracked.UntrackAction,
			task.TaskState_KILLED:      tracked.UntrackAction,
		},
		task.TaskState_KILLED: {
			task.TaskState_INITIALIZED: tracked.StopAction,
			task.TaskState_LAUNCHING:   tracked.StopAction,
			task.TaskState_LAUNCHED:    tracked.StopAction,
			task.TaskState_RUNNING:     tracked.StopAction,
			task.TaskState_KILLED:      tracked.UntrackAction,
			task.TaskState_SUCCEEDED:   tracked.UntrackAction,
			task.TaskState_FAILED:      tracked.UntrackAction,
		},
		task.TaskState_FAILED: {
			task.TaskState_FAILED:    tracked.UntrackAction,
			task.TaskState_SUCCEEDED: tracked.UntrackAction,
			task.TaskState_KILLED:    tracked.UntrackAction,
		},
	}
)

func (e *engine) processTask(t tracked.Task) {
	action := e.suggestTaskAction(t)
	lastAction, lastActionTime := t.LastAction()

	// Now run the action, to reflect the decision taken above.
	success := e.runTaskAction(action, t)

	// Update and reschedule the task, based on the result.
	delay := _indefDelay
	switch {
	case action == tracked.NoAction || action == tracked.UntrackAction:
		// No need to reschedule.

	case action != lastAction:
		// First time we see this, trigger default timeout.
		if success {
			delay = e.cfg.SuccessRetryDelay
		} else {
			delay = e.cfg.FailureRetryDelay
		}

	case action == lastAction:
		// Not the first time we see this, apply backoff.
		delay = time.Since(lastActionTime)
		if success {
			delay += e.cfg.SuccessRetryDelay
		} else {
			delay += e.cfg.FailureRetryDelay
		}
	}

	var deadline time.Time
	if delay != _indefDelay {
		// Cap delay to max.
		if delay > e.cfg.MaxRetryDelay {
			delay = e.cfg.MaxRetryDelay
		}
		deadline = time.Now().Add(delay)
	}

	e.trackedManager.ScheduleTask(t, deadline)
}

func (e *engine) runTaskAction(action tracked.TaskAction, t tracked.Task) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := t.RunAction(ctx, action)
	cancel()

	if err != nil {
		log.
			WithField("job_id", t.Job().ID().GetValue()).
			WithField("instance_id", t.ID()).
			WithField("action", action).
			WithError(err).
			Error("failed to execute goalstate action")
	}

	return err == nil
}

func (e *engine) suggestTaskAction(t tracked.Task) tracked.TaskAction {
	currentState := t.CurrentState()
	goalState := t.GoalState()

	// First test if the task is at the goal version. If not, we'll have to
	// trigger a stop and wait until the task is in a terminal state.
	if currentState.ConfigVersion != goalState.ConfigVersion {
		switch {
		case currentState.ConfigVersion == tracked.UnknownVersion,
			goalState.ConfigVersion == tracked.UnknownVersion:
			// Ignore versions if version is unknown.

		case util.IsPelotonStateTerminal(currentState.State):
			return tracked.UseGoalVersionAction

		default:
			return tracked.StopAction
		}
	}

	// At this point the job has the correct version.
	// Find action to reach goal state from current state.
	if tr, ok := _isoVersionsTaskRules[goalState.State]; ok {
		if a, ok := tr[currentState.State]; ok {
			return a
		}
	}

	return tracked.NoAction
}