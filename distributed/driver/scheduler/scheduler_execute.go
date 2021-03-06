package scheduler

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/chrislusf/gleam/distributed/netchan"
	"github.com/chrislusf/gleam/distributed/plan"
	"github.com/chrislusf/gleam/distributed/resource"
	"github.com/chrislusf/gleam/flow"
	"github.com/chrislusf/gleam/util"
)

func (s *Scheduler) remoteExecuteOnLocation(flowContext *flow.FlowContext, taskGroup *plan.TaskGroup, allocation resource.Allocation, wg *sync.WaitGroup) {
	// s.setupInputChannels(flowContext, tasks[0], allocation.Location, wg)

	// fmt.Printf("allocated %s on %v\n", tasks[0].Name(), allocation.Location)
	// create reqeust
	args := []string{}
	for _, arg := range os.Args[1:] {
		args = append(args, arg)
	}
	instructions := plan.TranslateToInstructionSet(taskGroup)
	firstInstruction := instructions.GetInstructions()[0]
	lastInstruction := instructions.GetInstructions()[len(instructions.GetInstructions())-1]
	firstTask := taskGroup.Tasks[0]
	lastTask := taskGroup.Tasks[len(taskGroup.Tasks)-1]
	var inputLocations, outputLocations []resource.DataLocation
	for _, shard := range firstTask.InputShards {
		loc, hasLocation := s.GetShardLocation(shard)
		if !hasLocation {
			log.Printf("The shard is missing?: %s", shard.Name())
			continue
		}
		inputLocations = append(inputLocations, resource.DataLocation{
			Name:     shard.Name(),
			Location: loc,
		})
	}
	for _, shard := range lastTask.OutputShards {
		outputLocations = append(outputLocations, resource.DataLocation{
			Name:     shard.Name(),
			Location: allocation.Location,
		})
	}

	firstInstruction.SetInputLocations(inputLocations)
	lastInstruction.SetOutputLocations(outputLocations)

	instructions.FlowHashCode = &flowContext.HashCode
	request := NewStartRequest(
		s.Option.Module,
		instructions,
		allocation.Allocated,
		os.Environ(),
		s.Option.DriverHost,
		int32(s.Option.DriverPort),
	)

	status, isOld := s.getRemoteExecutorStatus(instructions.HashCode())
	if isOld {
		log.Printf("Replacing old request: %v", status)
	}
	status.RequestTime = time.Now()
	status.Allocation = allocation
	status.Request = request
	taskGroup.RequestId = instructions.HashCode()

	// fmt.Printf("starting on %s: %v\n", allocation.Allocated, request)

	if err := RemoteDirectExecute(allocation.Location.URL(), request); err != nil {
		log.Printf("remote exeuction error %v: %v", err, request)
	}
	status.StopTime = time.Now()
}

func (s *Scheduler) localExecute(flowContext *flow.FlowContext, task *flow.Task, wg *sync.WaitGroup) {
	if task.Step.OutputDataset == nil {
		s.localExecuteOutput(flowContext, task, wg)
	} else {
		s.localExecuteSource(flowContext, task, wg)
	}
}

func (s *Scheduler) localExecuteSource(flowContext *flow.FlowContext, task *flow.Task, wg *sync.WaitGroup) {
	s.shardLocator.waitForOutputDatasetShardLocations(task)

	for _, shard := range task.OutputShards {
		location, _ := s.GetShardLocation(shard)
		shard.IncomingChan = util.NewPiper()
		wg.Add(1)
		go func() {
			// println(task.Step.Name, "writing to", shard.Name(), "at", location.URL())
			if err := netchan.DialWriteChannel(wg, "driver_input", location.URL(), shard.Name(), shard.IncomingChan.Reader, len(shard.ReadingTasks)); err != nil {
				println("starting:", task.Step.Name, "output location:", location.URL(), shard.Name(), "error:", err.Error())
			}
		}()
	}
	task.Step.RunFunction(task)
}

func (s *Scheduler) localExecuteOutput(flowContext *flow.FlowContext, task *flow.Task, wg *sync.WaitGroup) {
	s.shardLocator.waitForInputDatasetShardLocations(task)

	for i, shard := range task.InputShards {
		location, _ := s.GetShardLocation(shard)
		inChan := task.InputChans[i]
		wg.Add(1)
		go func() {
			// println(task.Step.Name, "reading from", shard.Name(), "at", location.URL(), "to", inChan)
			if err := netchan.DialReadChannel(wg, "driver_output", location.URL(), shard.Name(), inChan.Writer); err != nil {
				println("starting:", task.Step.Name, "input location:", location.URL(), shard.Name(), "error:", err.Error())
			}
		}()
	}
	task.Step.RunFunction(task)
}
