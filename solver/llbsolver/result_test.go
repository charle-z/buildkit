package llbsolver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/moby/buildkit/solver"
	llberrdefs "github.com/moby/buildkit/solver/llbsolver/errdefs"
	"github.com/stretchr/testify/require"
)

type countedResult struct {
	releases atomic.Int64
}

func (r *countedResult) ID() string { return "counted" }

func (r *countedResult) Release(context.Context) error {
	r.releases.Add(1)
	return nil
}

func (r *countedResult) Sys() any { return nil }

func (r *countedResult) Clone() solver.Result { return r }

func newExecError(ref solver.Result) error {
	return llberrdefs.WithExecError(errors.New("exec failed"), []solver.Result{ref}, nil)
}

func TestResultProxyAdoptExecErrorRefsBeforeRelease(t *testing.T) {
	ref := &countedResult{}
	rp := &resultProxy{}
	err := newExecError(ref)

	rp.adoptExecErrorRefs(err)

	require.Zero(t, ref.releases.Load())
	require.Len(t, rp.errResults, 1)
	require.NoError(t, rp.Release(t.Context()))
	require.Equal(t, int64(1), ref.releases.Load())
	require.Nil(t, rp.errResults)

	var execErr *llberrdefs.ExecError
	require.ErrorAs(t, err, &execErr)
	require.True(t, execErr.OwnerBorrowed)
}

func TestResultProxyAdoptExecErrorRefsAfterRelease(t *testing.T) {
	ref := &countedResult{}
	rp := &resultProxy{}
	err := newExecError(ref)

	require.NoError(t, rp.Release(t.Context()))
	rp.adoptExecErrorRefs(err)

	require.Equal(t, int64(1), ref.releases.Load())
	require.Nil(t, rp.errResults)
	var execErr *llberrdefs.ExecError
	require.ErrorAs(t, err, &execErr)
	require.True(t, execErr.OwnerBorrowed)

	require.NoError(t, rp.Release(t.Context()))
	require.Equal(t, int64(1), ref.releases.Load())
}

func TestResultProxyAdoptExecErrorRefsConcurrentWithRelease(t *testing.T) {
	for range 100 {
		ref := &countedResult{}
		rp := &resultProxy{}
		err := newExecError(ref)
		start := make(chan struct{})
		releaseErr := make(chan error, 1)
		ctx := t.Context()
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			rp.adoptExecErrorRefs(err)
		}()
		go func() {
			defer wg.Done()
			<-start
			releaseErr <- rp.Release(ctx)
		}()

		close(start)
		wg.Wait()

		require.NoError(t, <-releaseErr)
		require.Equal(t, int64(1), ref.releases.Load())
		require.Nil(t, rp.errResults)
	}
}
