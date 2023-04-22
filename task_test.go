package task

import (
	"context"
	"testing"
	//"github.com/stretchr/testify/assert"
)

func Permute[T any](l []T) [][]T {
	var out [][]T
	permute(l, 0, &out)
	return out
}
func permute[T any](l []T, i int, lp *[][]T) {
	if i+1 < len(l) {
		permute(l, i+1, lp)
		for j := i + 1; j < len(l); j++ {
			l2 := append([]T(nil), l...)
			l2[i], l2[j] = l2[j], l2[i]
			permute(l2, i+1, lp)
		}
	} else {
		*lp = append(*lp, l)
	}
}

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

permutation:
	for _, p := range Permute([]int{0, 1, 2}) {
		var tasks [3]*Task
		tasks[0] = With(ctx)
		tasks[1] = With(tasks[0])
		tasks[2] = With(tasks[1])

		for i := 0; i < 2; i++ {
			tasks[p[i]].Close()
			select {
			case <-tasks[0].Wait():
				t.Errorf("wait didn't block and should have. Closed=%+v", p[:i+1])
				continue permutation
			default:
			}
		}
		tasks[p[2]].Close()
		select {
		case <-tasks[0].Wait():
		default:
			t.Errorf("wait blocked and shouldn't have")
		}

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
