package flow

import (
	"io"

	"github.com/chrislusf/gleam/util"
)

func (d *Dataset) RoundRobin(shard int) *Dataset {
	if len(d.Shards) == shard {
		return d
	}
	ret := d.FlowContext.newNextDataset(shard)
	step := d.FlowContext.AddOneToAllStep(d, ret)
	step.Name = "RoundRobin"
	step.FunctionType = TypeRoundRobin
	step.Params["shardCount"] = len(ret.Shards)
	step.Function = func(readers []io.Reader, writers []io.Writer, task *Task) {
		RoundRobin(readers[0], writers)
	}
	return ret
}

// hash data or by data key, return a new dataset
// This is devided into 2 steps:
// 1. Each record is sharded to a local shard
// 2. The destination shard will collect its child shards and merge into one
func (d *Dataset) Partition(shard int, indexes ...int) *Dataset {
	if len(indexes) == 0 {
		indexes = []int{1}
	}
	if intArrayEquals(d.IsPartitionedBy, indexes) && shard == len(d.Shards) {
		return d
	}
	if 1 == len(d.Shards) && shard == 1 {
		return d
	}
	ret := d.partition_scatter(shard, indexes)
	if len(d.Shards) > 1 {
		ret = ret.partition_collect(shard, indexes)
	}
	ret.IsPartitionedBy = indexes
	return ret
}

func (d *Dataset) partition_scatter(shardCount int, indexes []int) (ret *Dataset) {
	ret = d.FlowContext.newNextDataset(len(d.Shards) * shardCount)
	step := d.FlowContext.AddOneToEveryNStep(d, shardCount, ret)
	step.Name = "Partition_scatter"
	step.Params["shardCount"] = shardCount
	step.Params["indexes"] = indexes
	step.FunctionType = TypeScatterPartitions
	step.Function = func(readers []io.Reader, writers []io.Writer, task *Task) {
		ScatterPartitions(readers[0], writers, indexes)
	}
	return
}

func (d *Dataset) partition_collect(shardCount int, indexes []int) (ret *Dataset) {
	ret = d.FlowContext.newNextDataset(shardCount)
	step := d.FlowContext.AddLinkedNToOneStep(d, len(d.Shards)/shardCount, ret)
	step.Name = "Partition_collect"
	step.FunctionType = TypeCollectPartitions
	step.Function = func(readers []io.Reader, writers []io.Writer, task *Task) {
		CollectPartitions(readers, writers[0])
	}
	return
}

func ScatterPartitions(reader io.Reader, writers []io.Writer, indexes []int) {
	shardCount := len(writers)

	util.ProcessMessage(reader, func(data []byte) error {
		keyObjects, _ := util.DecodeRowKeys(data, indexes)
		x := util.PartitionByKeys(shardCount, keyObjects)
		util.WriteMessage(writers[x], data)
		return nil
	})
}

func RoundRobin(reader io.Reader, writers []io.Writer) {
	count, shardCount := 0, len(writers)
	util.ProcessMessage(reader, func(data []byte) error {
		if count >= shardCount {
			count = 0
		}
		util.WriteMessage(writers[count], data)
		count++
		return nil
	})
}

func CollectPartitions(readers []io.Reader, writer io.Writer) {
	// println("starting to collect data from partitions...", len(readers))

	if len(readers) == 1 {
		io.Copy(writer, readers[0])
		return
	}

	util.CopyMultipleReaders(readers, writer)
}

func intArrayEquals(a []int, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
