package task

import (
	"context"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"
)

type TaskError struct {
	taskName string
	err      error
}

func (te TaskError) Error() string {
	return "[" + te.taskName + "] " + te.err.Error()
}

func (te TaskError) Unwrap() error {
	return te.err
}

type Task struct {
	ctx        context.Context
	cancelFunc func()
	waitChan   chan struct{}
	name       string

	parent   *Task
	children syncMap[*Task, struct{}]

	closed     bool
	error      error
	errorChan  chan error
	childTotal int
	mutex      sync.Mutex
}
type taskContextKey struct{}

func (t Task) Deadline() (time.Time, bool) { return t.ctx.Deadline() }

// Done is used by the worker to detect when it should abort and exit.
// Also signals when the task and all its descendants have closed.
func (t Task) Done() <-chan struct{}             { return t.ctx.Done() }
func (t Task) Err() error                        { return t.ctx.Err() }
func (t Task) Value(key interface{}) interface{} { return t.ctx.Value(key) }

// Cancel sends a notification to the descendant worker(s) to abort and exit.
func (t Task) Cancel() { t.cancelFunc() }

// Wait is used to block until the task and all its descendant workers have closed.
func (t Task) Wait() <-chan struct{} { return t.waitChan }

// Name returns the name for this task.
func (t *Task) Name() string {
	return t.name
}

// Waiting returns a list of names of descendant tasks which are still running.
func (t *Task) Waiting() []string {
	var names []string
	t.children.Range(func(ct *Task, _ struct{}) bool {
		names = append(names, ct.Name())
		for _, name := range ct.Waiting() {
			names = append(names, ct.Name()+"."+name)
		}
		return true
	})
	return names
}

// Init applies the context labels to the current goroutine. Returns the Task for convenient chaining.
// Example usage:
//
//	go func(tsk *task.Task) {
//	  defer tsk.Init().Close()
//	  ...
//	}(task.WithName(tsk, "server"))
func (t *Task) Init() *Task {
	pprof.SetGoroutineLabels(t)
	return t
}

// Close sends a notification to the parent(s) that the worker has finished.
// Note that the parent won't receive the notification until all descendants have closed as well.
// If a non-nil error has been provided, it will be sent to the parent.
func (t *Task) Close() {
	t.CloseErr(nil)
}

// CloseErr performs the same function as Close() with the addition that the provided error will be sent to the parent
// context.
// Errors are not bubbled up the ancestry tree. To do so, the parent must explicitly receive and pass the error along.
func (t *Task) CloseErr(err error) {
	t.mutex.Lock()
	if t.closed {
		t.mutex.Unlock()
		panic("task already closed")
	}
	t.error = err
	t.closed = true
	t.fullClose()
	t.sendError(err)
	t.mutex.Unlock()
}

// CloseErrP is a convenience wrapper around CloseErr() to allow a pointer to an error.
// The intent is to simplify the use of `defer` by taking advantage of named return parameters. For example:
//
//	tsk := task.With(context.Background)
//	go func() (err error) {
//	  defer tsk.CloseErrP(&err)
//	  ...
//	  return errors.New("something bad!")
//	}()
func (t *Task) CloseErrP(err *error) {
	if err != nil {
		t.CloseErr(*err)
	} else {
		t.CloseErr(nil)
	}
}

func (t *Task) removeChild(tsk *Task) {
	t.children.Delete(tsk)
	t.fullClose()
}

func (t *Task) fullClose() {
	if !t.closed || !t.children.IsEmpty() {
		return
	}
	close(t.waitChan)
	if t.errorChan != nil {
		close(t.errorChan)
	}
	// t.Cancel() to signal Done() in case anything tries to check it.
	// Also to unblock the goroutines that get created by context.WithCancel().
	t.Cancel()
	if t.parent != nil {
		t.parent.removeChild(t)
	}
}

func (t *Task) sendError(err error) {
	t.mutex.Lock()
	if t.errorChan != nil {
		t.errorChan <- err
	}
	t.mutex.Unlock()
	if t.parent != nil {
		if tskErr, ok := err.(TaskError); ok {
			err = TaskError{
				taskName: t.Name() + "." + tskErr.taskName,
				err:      err,
			}
		}
		t.parent.sendError(err)
	}
}

// Errors provides a chan which streams all the errors from the task and its descendants.
// Errors must be called before any descendants return errors for them to be provided. Errors does not need to be called
// beforehand to provide the error from this task.
func (t *Task) Errors() <-chan error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.errorChan == nil {
		t.errorChan = make(chan error, 1)
		if t.error != nil {
			t.errorChan <- t.error
		}
	}
	return t.errorChan
}

// With creates a new Task using parentCtx as the parent context.
// If the parent or any ancestor is another Task, any reads on that Task's Wait chan will block until this Task's Close is called.
// If the parent/ancestor is already closed, any waiters will not wait for this new child.
func With(parentCtx context.Context, labels ...string) *Task {
	return WithName(parentCtx, "", labels...)
}

// WithName creates a new Task using parentCtx as the parent context.
// The name is used to identify which task an error came from, and for debug labeling of goroutines (pprof Labels).
// If the parent or any ancestor is another Task, any reads on that Task's Wait chan will block until this Task's Close or Cancel is called.
// If the parent/ancestor is already closed, any waiters will not wait for this new child.
func WithName(parentCtx context.Context, name string, labels ...string) *Task {
	t := &Task{
		name:     name,
		waitChan: make(chan struct{}),
	}

	if parentCtx == nil {
		parentCtx = context.Background()
		if t.name == "" {
			t.name = "unknown"
		}
	} else {
		parentTask, ok := parentCtx.(*Task)
		if !ok {
			parentTask, _ = parentCtx.Value(taskContextKey{}).(*Task)
		}
		if parentTask != nil {
			parentTask.mutex.Lock()
			if !parentTask.closed {
				parentTask.children.Store(t, struct{}{})
			}
			t.parent = parentTask
			if t.name == "" {
				t.name = strconv.Itoa(parentTask.childTotal)
			}
			parentTask.childTotal++
			parentTask.mutex.Unlock()
		} else {
			if t.name == "" {
				t.name = "unknown"
			}
		}
	}

	t.ctx, t.cancelFunc = context.WithCancel(parentCtx)
	t.ctx = context.WithValue(t.ctx, taskContextKey{}, t)
	t.ctx = pprof.WithLabels(t.ctx, pprof.Labels(append([]string{"_task", t.name}, labels...)...))

	return t
}
