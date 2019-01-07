// Copyright (c) 2019 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package queue

import (
	"testing"

	"github.com/uber/peloton/.gen/peloton/api/v0/respool"

	"github.com/stretchr/testify/suite"
)

// QueueTestSuite is the struct for Queue Tests
type QueueTestSuite struct {
	suite.Suite
}

func TestQueue(t *testing.T) {
	suite.Run(t, new(QueueTestSuite))
}

// TestCreateQueue tests the Create Queue
func (suite *QueueTestSuite) TestCreateQueueSuccess() {
	q, err := CreateQueue(respool.SchedulingPolicy_PriorityFIFO, 100)
	suite.NoError(err)
	suite.NotNil(q)
}

// TestCreateQueue tests the Create Queue
func (suite *QueueTestSuite) TestCreateQueueError() {
	q, err := CreateQueue(2, 100)
	suite.Nil(q)
	suite.Error(err)
	suite.EqualError(err, "invalid queue type")
}
