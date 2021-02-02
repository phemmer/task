package task

import (
	"context"
	"sync"
	"time"
)

type Task struct {
	ctx        context.Context
	cancelFunc func()
	waitChan   chan struct{}

	parentTask *Task

	closed     bool
	errors []error
	// errorChan is initialized on first call to WorkErrors(), upon which it's sized to the number of errors + outstanding
	// children.
	errorChan chan error
	childCount int
	mutex      sync.Mutex
}
type workerContextKey struct{}

func (t Task) Deadline() (time.Time, bool)       { return t.ctx.Deadline() }

// Done is used by the worker to detect when it should abort and exit.
// Also signals when the task and all its descendants have closed.
func (t Task) Done() <-chan struct{}             { return t.ctx.Done() }
func (t Task) Err() error                        { return t.ctx.Err() }
func (t Task) Value(key interface{}) interface{} { return t.ctx.Value(key) }

// Cancel sends a notification to the descendant worker(s) to abort and exit.
func (t Task) Cancel()               { t.cancelFunc() }

// Wait is used to block until the task and all its descendant workers have closed.
func (t Task) Wait() <-chan struct{} { return t.waitChan }

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
		panic("worker already closed")
	}
	t.closed = true
	if t.parentTask != nil {
		t.parentTask.addChildError(err)
	}
	t.mutex.Unlock()
	t.closeChild()
}

// CloseErrP is a convenience wrapper around CloseErr() to allow a pointer to an error.
// The intent is to simplify the use of `defer` by taking advantage of named return parameters. For example:
//  tsk := task.With(context.Background)
//  go func() (err error) {
//    defer tsk.CloseErrP(&err)
//    ...
//    return errors.New("something bad!")
//  }()
func (t *Task) CloseErrP(err *error) {
	if err != nil {
		t.CloseErr(*err)
	} else {
		t.CloseErr(nil)
	}
}

func (t *Task) closeChild() {
	t.mutex.Lock()
	t.childCount--
	if t.childCount == 0 {
		close(t.waitChan)
		if t.errorChan != nil {
			close(t.errorChan)
		}
		// t.Cancel() to signal Done() in case anything tries to check it.
		// Also to unblock the goroutines that get created by context.WithCancel().
		t.Cancel()
		if t.parentTask != nil {
			t.parentTask.closeChild()
		}
	}
	t.mutex.Unlock()
}

func (t *Task) addChildError(err error) {
	t.mutex.Lock()
	t.errors = append(t.errors, err)
	if t.errorChan != nil {
		t.errorChan <- err
	}
	t.mutex.Unlock()
}

// Waits for all chilren to complete, and provides the first, if any, error.
func (t *Task) WaitError() error {
	<-t.Wait()
	if len(t.errors) == 0 {
		return nil
	}
	return t.errors[0]
}

// Provides all the errors returned by child workers.
func (t *Task) WorkErrors() <-chan error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.errorChan == nil {
		t.errorChan = make(chan error, len(t.errors) + t.childCount)
		for _, err := range t.errors {
			t.errorChan <- err
		}
		if t.childCount == 0 {
			close(t.errorChan)
		}
	}
	return t.errorChan
}

// Worker is a convenience function to return t as interface Worker.
func (t *Task) Worker() Worker       { return t }

// Ctx is a convenience function to return t as interface context.Context.
func (t *Task) Ctx() context.Context { return t }

// NewWorker is a convenience function for task.With(t)
func (t *Task) NewWorker() Worker    { return With(t) }

// With creates a new Task using parentCtx as the parent context.
//
// If the parent worker has already closed, any waiters on the parent will not wait for this new child worker.
//
// The worker must ensure to call Close() on the task when exiting.
func With(parentCtx context.Context) *Task {
	t := &Task{
		waitChan:   make(chan struct{}),
		childCount: 1,
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	} else {
		parentWorker, ok := parentCtx.(*Task)
		if !ok {
			parentWorker, _ = parentCtx.Value(workerContextKey{}).(*Task)
		}
		if parentWorker != nil {
			parentWorker.mutex.Lock()
			if parentWorker.childCount > 0 {
				parentWorker.childCount++
				t.parentTask = parentWorker
			}
			parentWorker.mutex.Unlock()
		}
	}

	t.ctx, t.cancelFunc = context.WithCancel(parentCtx)
	t.ctx = context.WithValue(t.ctx, workerContextKey{}, t)

	return t
}

type Worker interface {
	Deadline() (time.Time, bool)
	// Done is used by the worker to detect when it should abort and exit.
	Done() <-chan struct{}
	Err() error
	Value(key interface{}) interface{}
	// Close sends a notification to the parent(s) that the worker has finished.
	Close()
	// CloseErr sends a notification & error to the parent(s) that the worker has finished.
	CloseErr(error)
	// CloseErrP sends a notification & error to the parent(s) that the worker has finished.
	CloseErrP(*error)
	Ctx() context.Context
	// NewWorker is a convenience function for task.With(t)
	NewWorker() Worker
}
