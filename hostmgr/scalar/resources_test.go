package scalar

import (
	"testing"

	mesos "code.uber.internal/infra/peloton/.gen/mesos/v1"
	"code.uber.internal/infra/peloton/.gen/peloton/api/v0/task"

	"code.uber.internal/infra/peloton/util"
	"github.com/stretchr/testify/assert"
)

const zeroEpsilon = 0.000001

func createResource(cpus, gpus, mem, disk float64) Resources {
	return Resources{
		CPU:  cpus,
		Mem:  mem,
		Disk: disk,
		GPU:  gpus,
	}
}

func TestContains(t *testing.T) {
	// An empty Resources should container another empty one.
	empty1 := Resources{}
	empty2 := Resources{}
	assert.True(t, empty1.Contains(empty1))
	assert.True(t, empty1.Contains(empty2))

	r1 := Resources{
		CPU: 1.0,
	}
	assert.True(t, r1.Contains(r1))
	assert.False(t, empty1.Contains(r1))
	assert.True(t, r1.Contains(empty1))

	r2 := Resources{
		Mem: 1.0,
	}
	assert.False(t, r1.Contains(r2))
	assert.False(t, r2.Contains(r1))

	r3 := Resources{
		CPU:  1.0,
		Mem:  1.0,
		Disk: 1.0,
		GPU:  1.0,
	}
	assert.False(t, r1.Contains(r3))
	assert.False(t, r2.Contains(r3))
	assert.True(t, r3.Contains(r1))
	assert.True(t, r3.Contains(r2))
	assert.True(t, r3.Contains(r3))
}

func TestHasGPU(t *testing.T) {
	empty := Resources{}
	assert.False(t, empty.HasGPU())

	r1 := Resources{
		CPU: 1.0,
	}
	assert.False(t, r1.HasGPU())

	r2 := Resources{
		CPU: 1.0,
		GPU: 1.0,
	}
	assert.True(t, r2.HasGPU())
}

func TestAdd(t *testing.T) {
	empty := Resources{}
	r1 := Resources{
		CPU: 1.0,
	}

	result := empty.Add(empty)
	assert.InEpsilon(t, 0.0, result.CPU, zeroEpsilon)
	assert.InEpsilon(t, 0.0, result.Mem, zeroEpsilon)
	assert.InEpsilon(t, 0.0, result.Disk, zeroEpsilon)
	assert.InEpsilon(t, 0.0, result.GPU, zeroEpsilon)

	result = r1.Add(Resources{})
	assert.InEpsilon(t, 1.0, result.CPU, zeroEpsilon)
	assert.InEpsilon(t, 0.0, result.Mem, zeroEpsilon)
	assert.InEpsilon(t, 0.0, result.Disk, zeroEpsilon)
	assert.InEpsilon(t, 0.0, result.GPU, zeroEpsilon)

	r2 := Resources{
		CPU:  4.0,
		Mem:  3.0,
		Disk: 2.0,
		GPU:  1.0,
	}
	result = r1.Add(r2)
	assert.InEpsilon(t, 5.0, result.CPU, zeroEpsilon)
	assert.InEpsilon(t, 3.0, result.Mem, zeroEpsilon)
	assert.InEpsilon(t, 2.0, result.Disk, zeroEpsilon)
	assert.InEpsilon(t, 1.0, result.GPU, zeroEpsilon)
}

func TestTrySubtract(t *testing.T) {
	empty := Resources{}
	r1 := Resources{
		CPU:  1.0,
		Mem:  2.0,
		Disk: 3.0,
		GPU:  4.0,
	}

	res, ok := empty.TrySubtract(empty)
	assert.True(t, ok)
	assert.InEpsilon(t, 0.0, res.CPU, zeroEpsilon)
	assert.InEpsilon(t, 0.0, res.Mem, zeroEpsilon)
	assert.InEpsilon(t, 0.0, res.Disk, zeroEpsilon)
	assert.InEpsilon(t, 0.0, res.GPU, zeroEpsilon)

	_, ok = empty.TrySubtract(r1)
	assert.False(t, ok)

	r2 := r1
	res, ok = r2.TrySubtract(r1)
	assert.True(t, ok)
	assert.InEpsilon(t, 0.0, res.CPU, zeroEpsilon)
	assert.InEpsilon(t, 0.0, res.Mem, zeroEpsilon)
	assert.InEpsilon(t, 0.0, res.Disk, zeroEpsilon)
	assert.InEpsilon(t, 0.0, res.GPU, zeroEpsilon)

	res, ok = r1.TrySubtract(empty)
	assert.True(t, ok)
	assert.InEpsilon(t, 1.0, res.CPU, zeroEpsilon)
	assert.InEpsilon(t, 2.0, res.Mem, zeroEpsilon)
	assert.InEpsilon(t, 3.0, res.Disk, zeroEpsilon)
	assert.InEpsilon(t, 4.0, res.GPU, zeroEpsilon)

	r3 := Resources{
		CPU:  5.0,
		Mem:  6.0,
		Disk: 7.0,
		GPU:  8.0,
	}
	res, ok = r3.TrySubtract(r1)
	assert.NotNil(t, res)
	assert.InEpsilon(t, 4.0, res.CPU, zeroEpsilon)
	assert.InEpsilon(t, 4.0, res.Mem, zeroEpsilon)
	assert.InEpsilon(t, 4.0, res.Disk, zeroEpsilon)
	assert.InEpsilon(t, 4.0, res.GPU, zeroEpsilon)

	// r3 is more than r1
	_, ok = r1.TrySubtract(r3)
	assert.False(t, ok)
}

func TestFromOfferMap(t *testing.T) {
	rs := []*mesos.Resource{
		util.NewMesosResourceBuilder().WithName("cpus").WithValue(1.0).Build(),
		util.NewMesosResourceBuilder().WithName("mem").WithValue(2.0).Build(),
		util.NewMesosResourceBuilder().WithName("disk").WithValue(3.0).Build(),
		util.NewMesosResourceBuilder().WithName("gpus").WithValue(4.0).Build(),
		util.NewMesosResourceBuilder().WithName("custom").WithValue(5.0).Build(),
	}

	offer := mesos.Offer{
		Resources: rs,
	}

	result := FromOfferMap(map[string]*mesos.Offer{"o1": &offer})
	assert.InEpsilon(t, 1.0, result.CPU, zeroEpsilon)
	assert.InEpsilon(t, 2.0, result.Mem, zeroEpsilon)
	assert.InEpsilon(t, 3.0, result.Disk, zeroEpsilon)
	assert.InEpsilon(t, 4.0, result.GPU, zeroEpsilon)

	result = FromOfferMap(map[string]*mesos.Offer{
		"o1": &offer,
		"o2": &offer,
	})
	assert.InEpsilon(t, 2.0, result.CPU, zeroEpsilon)
	assert.InEpsilon(t, 4.0, result.Mem, zeroEpsilon)
	assert.InEpsilon(t, 6.0, result.Disk, zeroEpsilon)
	assert.InEpsilon(t, 8.0, result.GPU, zeroEpsilon)
}

func TestFromResourceConfig(t *testing.T) {
	result := FromResourceConfig(&task.ResourceConfig{
		CpuLimit:    1.0,
		MemLimitMb:  2.0,
		DiskLimitMb: 3.0,
		GpuLimit:    4.0,
	})
	assert.InEpsilon(t, 1.0, result.CPU, zeroEpsilon)
	assert.InEpsilon(t, 2.0, result.Mem, zeroEpsilon)
	assert.InEpsilon(t, 3.0, result.Disk, zeroEpsilon)
	assert.InEpsilon(t, 4.0, result.GPU, zeroEpsilon)
}

func TestMinimum(t *testing.T) {
	r1 := Minimum(
		Resources{
			CPU: 1.0,
			Mem: 2.0,
		},
		Resources{
			CPU:  2.0,
			Mem:  2.0,
			Disk: 2.0,
		},
	)

	assert.InEpsilon(t, 1.0, r1.CPU, zeroEpsilon)
	assert.InEpsilon(t, 2.0, r1.Mem, zeroEpsilon)
	assert.InEpsilon(t, 0.0, r1.Disk, zeroEpsilon)
	assert.InEpsilon(t, 0.0, r1.GPU, zeroEpsilon)
}

func TestNonEmptyFields(t *testing.T) {
	r1 := Resources{}
	assert.True(t, r1.Empty())
	assert.Empty(t, r1.NonEmptyFields())

	r2 := Resources{
		CPU:  1.0,
		Disk: 2.0,
	}
	assert.False(t, r2.Empty())
	assert.Equal(t, []string{"cpus", "disk"}, r2.NonEmptyFields())
}

func TestScarceResourceType(t *testing.T) {
	testTable := []struct {
		scarceResourceType []string
		reqResource        Resources
		agentResources     Resources
		expected           bool
		msg                string
	}{
		{
			msg:                "GPU task can schedule on GPU machine",
			scarceResourceType: []string{"GPU"},
			reqResource:        createResource(1.0, 1.0, 100.0, 100.0),
			agentResources:     createResource(24.0, 4.0, 10000.0, 100000.0),
			expected:           true,
		},
		{
			msg:                "non-GPU task can not schedule on GPU machine",
			scarceResourceType: []string{"GPU"},
			reqResource:        createResource(1.0, 0, 100.0, 100.0),
			agentResources:     createResource(24.0, 4.0, 10000.0, 100000.0),
			expected:           false,
		},
	}

	for _, tt := range testTable {
		assert.Equal(t, tt.reqResource.GetCPU(), 1.0)
		assert.Equal(t, tt.reqResource.GetMem(), 100.0)
		assert.Equal(t, tt.reqResource.GetDisk(), 100.0)
		assert.Equal(t, tt.agentResources.GetGPU(), 4.0)
		for _, resourceType := range tt.scarceResourceType {
			assert.NotEqual(t, HasResourceType(tt.agentResources, tt.reqResource, resourceType), tt.expected)
		}
	}
}

func TestFromTaskResources(t *testing.T) {
	r := FromTaskResources(&task.ResourceConfig{
		CpuLimit:    1,
		DiskLimitMb: 1,
		GpuLimit:    1,
		MemLimitMb:  1,
	})
	assert.Equal(t, r.CPU, 1.0)
	assert.Equal(t, r.Mem, 1.0)
	assert.Equal(t, r.Disk, 1.0)
	assert.Equal(t, r.GPU, 1.0)
}
