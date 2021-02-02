package task

import (
	"context"
	"testing"
	//"github.com/stretchr/testify/assert"
)

func TestWait(t *testing.T) {
	ctx := context.Background()
	tsk := With(ctx)
	tsk.Close()
	select {
	case <-tsk.Wait():
	default:
		t.Errorf("task not closed")
	}
}

func TestWaitNested(t *testing.T) {
	ctx := context.Background()

	tsk1 := With(ctx)
	tsk2 := With(tsk1)
	tsk3 := With(tsk1)
	tsk2.Close()
	tsk3.Close()
	select {
	case <-tsk1.Wait():
		t.Errorf("task is closed")
	default:
	}
	tsk1.Close()
	select {
	case <-tsk1.Wait():
	default:
		t.Errorf("task not closed")
	}

	// flip the order and try again
	tsk1 = With(ctx)
	tsk2 = With(tsk1)
	tsk1.Close()
	select {
	case <-tsk1.Wait():
		t.Errorf("task is closed")
	default:
	}
	tsk2.Close()
	select {
	case <-tsk1.Wait():
	default:
		t.Errorf("task not closed")
	}
}

func TestWaitNestedApart(t *testing.T) {
	ctx := context.Background()

	tsk1 := With(ctx)
	ctx, _ = context.WithCancel(ctx)
	ctx = context.WithValue(ctx, "foo", "bar")
	tsk2 := With(ctx)

	tsk2.Close()
	select {
	case <-tsk1.Wait():
		t.Errorf("task is closed")
	default:
	}

	tsk1.Close()
	select {
	case <-tsk1.Wait():
	default:
		t.Errorf("task not closed")
	}
}

func TestCloseDone(t *testing.T) {
	ctx := context.Background()
	tsk := With(ctx)
	tsk2 := With(tsk)

	tsk.Close()
	select {
	case <-tsk.Done():
		// we haven't closed the child, so it should not be done
		t.Errorf("task is done")
	default:
	}

	tsk2.Close()
	select {
	case <-tsk2.Done():
	default:
		t.Errorf("task not done")
	}
	select {
	case <-tsk.Done():
	default:
		t.Errorf("task not done")
	}
}

func TestCancelWait(t *testing.T) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	tsk := With(ctx)
	cancel()

	select {
	case <-tsk.Done():
	default:
		t.Errorf("task not cancelled")
	}

	select {
	case <-tsk.Wait():
		t.Errorf("task is closed")
	default:
	}
}

func TestCancelPropogation(t *testing.T) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	tsk1 := With(ctx)
	tsk2 := With(tsk1)

	select {
	case <-tsk1.Done():
		t.Errorf("task is cancelled")
	case <-tsk2.Done():
		t.Errorf("task is cancelled")
	default:
	}

	cancel()

	select {
	case <-tsk1.Done():
	case <-tsk2.Done():
	default:
		t.Errorf("task not cancelled")
	}
}
