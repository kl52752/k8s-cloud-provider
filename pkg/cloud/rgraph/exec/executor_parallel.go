/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package exec

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/rgraph/algo"
	"k8s.io/klog/v2"
)

var (
	ErrPendingActions = errors.New("Executor did not process all actions")
)

func defaultParallelExecutorConfig() *ExecutorConfig {
	return &ExecutorConfig{
		DryRun:        false,
		ErrorStrategy: ContinueOnError,
		Timeout:       5 * time.Minute,
	}
}

// NewParallelExecutor returns a new Executor that runs tasks multi-threaded.
func NewParallelExecutor(c cloud.Cloud, pending []Action, opts ...Option) (*parallelExecutor, error) {
	ret := &parallelExecutor{
		config: defaultParallelExecutorConfig(),
		cloud:  c,
		result: &Result{Pending: pending},
		pq:     algo.NewParallelQueue[Action](),
	}
	for _, opt := range opts {
		opt(ret.config)
	}

	if err := ret.config.validate(); err != nil {
		return nil, err
	}
	return ret, nil
}

type parallelExecutor struct {
	config *ExecutorConfig
	cloud  cloud.Cloud

	// lock guards results
	lock   sync.Mutex
	result *Result

	pq   *algo.ParallelQueue[Action]
	done chan *TraceEntry
}

// parallelExecutor implements Executor.
var _ Executor = (*parallelExecutor)(nil)

// Run executes pending actions in parallel.
func (ex *parallelExecutor) Run(ctx context.Context) (*Result, error) {
	ex.queueRunnableActions()
	subctx, cancel := context.WithTimeout(ctx, ex.config.Timeout)

	queueErr := ex.pq.Run(subctx, ex.runAction)
	klog.Infof("ex.pq.Run(subctx, _): %v", queueErr)
	cancel()
	if queueErr != nil {
		klog.Infof("queue returned error %v. Start WaitForOrphans(ctx, _)", queueErr)
		waitErr := ex.pq.WaitForOrphans(ctx)
		klog.Infof("ex.pq.WaitForOrphans(ctx, _): %v", waitErr)
		if waitErr != nil {
			return ex.result, fmt.Errorf("ParallelExecutor: WaitForOrphans: %w", waitErr)
		}
	}
	if len(ex.result.Errors) > 0 || len(ex.result.Pending) != 0 {
		return ex.result, ErrPendingActions
	}
	return ex.result, nil
}

// Run executes pending actions in parallel this method does not cancel the context on timeout.
func (ex *parallelExecutor) RunWithoutConfigTimeout(ctx context.Context) (*Result, error) {
	ex.queueRunnableActions()
	queueErr := ex.pq.Run(ctx, ex.runAction)
	klog.Infof("ex.pq.Run(ctx, _): %v", queueErr)
	if queueErr != nil {
		// This call does not make sense because it will return immediately with error.
		// That means that we will not wait for children and lose the information if everything is finished.
		klog.Infof("queue returned error %v. Start WaitForOrphans(ctx, _)", queueErr)
		waitErr := ex.pq.WaitForOrphans(ctx)
		klog.Infof("ex.pq.WaitForOrphans(ctx, _): %v", waitErr)
		if waitErr != nil {
			return ex.result, fmt.Errorf("ParallelExecutor: WaitForOrphans: %w", waitErr)
		}
	}
	if len(ex.result.Errors) > 0 || len(ex.result.Pending) != 0 {
		return ex.result, ErrPendingActions
	}
	return ex.result, nil
}

func (ex *parallelExecutor) runAction(ctx context.Context, a Action) error {
	te := &TraceEntry{
		Action: a,
		Start:  time.Now(),
	}
	klog.V(4).Infof("Run action %s", a)
	events, runErr := a.Run(ctx, ex.cloud)
	te.End = time.Now()
	klog.V(4).Infof("Finish action %s, err: %v", a, runErr)

	ex.addActionResult(a, runErr)

	if runErr != nil {
		klog.Infof("Got error  %v, from action %s error_strategy: %s", runErr, a, ex.config.ErrorStrategy)
		// check error strategy and decide if new actions should be executed.
		if ex.config.ErrorStrategy == StopOnError {
			if ex.config.Tracer != nil {
				ex.config.Tracer.Record(te, runErr)
			}
			return fmt.Errorf("parallelExecutor: StopOnError due to Action %s: %w", a, runErr)
		}
	} else {
		// notify parents only when action finished with success
		te.Signaled = ex.signal(events)
	}

	if ex.config.Tracer != nil {
		ex.config.Tracer.Record(te, runErr)
	}

	// try to run pending tasks
	ex.queueRunnableActions()
	return nil
}

func (ex *parallelExecutor) queueRunnableActions() {
	ex.lock.Lock()
	defer ex.lock.Unlock()

	klog.Infof("queueRunnableActions: %d actions pending", len(ex.result.Pending))

	taskWasRun := false
	var notRunnable []Action
	for _, a := range ex.result.Pending {
		if a.CanRun() {
			klog.Infof("Run task: %s", a)
			ex.pq.Add(a)
			taskWasRun = true
		} else {
			notRunnable = append(notRunnable, a)
		}
	}
	klog.Infof("queueRunnableActions: remaining %d pending actions", len(notRunnable))
	// update Pending array only if actions were run
	if taskWasRun {
		ex.result.Pending = notRunnable
	}
}

// signal notifies parents that action finished
func (ex *parallelExecutor) signal(evs []Event) []TraceSignal {
	ex.lock.Lock()
	defer ex.lock.Unlock()
	var ret []TraceSignal
	for _, a := range ex.result.Pending {
		for _, ev := range evs {
			if a.Signal(ev) {
				ret = append(ret, TraceSignal{Event: ev, SignaledAction: a})
			}
		}
	}
	return ret
}

func (ex *parallelExecutor) addActionResult(a Action, runErr error) {
	ex.lock.Lock()
	defer ex.lock.Unlock()
	if runErr == nil {
		ex.result.Completed = append(ex.result.Completed, a)
	} else {
		ex.result.Errors = append(ex.result.Errors, ActionWithErr{Action: a, Err: runErr})
	}
}
