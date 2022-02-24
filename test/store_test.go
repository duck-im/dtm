package test

import (
	"fmt"
	"testing"
	"time"

	"github.com/dtm-labs/dtm/dtmcli/dtmimp"
	"github.com/dtm-labs/dtm/dtmsvr/storage"
	"github.com/dtm-labs/dtm/dtmsvr/storage/registry"
	"github.com/dtm-labs/dtm/dtmutil"
	"github.com/stretchr/testify/assert"
)

func initTransGlobal(gid string) (*storage.TransGlobalStore, storage.Store) {
	next := time.Now().Add(10 * time.Second)
	return initTransGlobalByNextCronTime(gid, next)
}

func initTransGlobalByNextCronTime(gid string, next time.Time) (*storage.TransGlobalStore, storage.Store) {
	g := &storage.TransGlobalStore{Gid: gid, Status: "prepared", NextCronTime: &next}
	bs := []storage.TransBranchStore{
		{Gid: gid, BranchID: "01"},
	}
	s := registry.GetStore()
	err := s.MaySaveNewTrans(g, bs)
	dtmimp.E2P(err)
	return g, s
}

func TestStoreSave(t *testing.T) {
	gid := dtmimp.GetFuncName()
	bs := []storage.TransBranchStore{
		{Gid: gid, BranchID: "01"},
		{Gid: gid, BranchID: "02"},
	}
	g, s := initTransGlobal(gid)
	g2 := s.FindTransGlobalStore(gid)
	assert.NotNil(t, g2)
	assert.Equal(t, gid, g2.Gid)

	bs2 := s.FindBranches(gid)
	assert.Equal(t, len(bs2), int(1))
	assert.Equal(t, "01", bs2[0].BranchID)

	s.LockGlobalSaveBranches(gid, g.Status, []storage.TransBranchStore{bs[1]}, -1)
	bs3 := s.FindBranches(gid)
	assert.Equal(t, 2, len(bs3))
	assert.Equal(t, "02", bs3[1].BranchID)
	assert.Equal(t, "01", bs3[0].BranchID)

	err := dtmimp.CatchP(func() {
		s.LockGlobalSaveBranches(g.Gid, "submitted", []storage.TransBranchStore{bs[1]}, 1)
	})
	assert.Equal(t, storage.ErrNotFound, err)

	s.ChangeGlobalStatus(g, "succeed", []string{}, true)
}

func TestStoreChangeStatus(t *testing.T) {
	gid := dtmimp.GetFuncName()
	g, s := initTransGlobal(gid)
	g.Status = "no"
	err := dtmimp.CatchP(func() {
		s.ChangeGlobalStatus(g, "submitted", []string{}, false)
	})
	assert.Equal(t, storage.ErrNotFound, err)
	g.Status = "prepared"
	s.ChangeGlobalStatus(g, "submitted", []string{}, false)
	s.ChangeGlobalStatus(g, "succeed", []string{}, true)
}

func TestStoreLockTrans(t *testing.T) {
	// lock trans will only lock unfinished trans. ensure all other trans are finished
	gid := dtmimp.GetFuncName()
	g, s := initTransGlobal(gid)

	g2 := s.LockOneGlobalTrans(2 * time.Duration(conf.RetryInterval) * time.Second)
	assert.NotNil(t, g2)
	assert.Equal(t, gid, g2.Gid)

	s.TouchCronTime(g, 3*conf.RetryInterval, dtmutil.GetNextTime(3*conf.RetryInterval))
	g2 = s.LockOneGlobalTrans(2 * time.Duration(conf.RetryInterval) * time.Second)
	assert.Nil(t, g2)

	s.TouchCronTime(g, 1*conf.RetryInterval, dtmutil.GetNextTime(1*conf.RetryInterval))
	g2 = s.LockOneGlobalTrans(2 * time.Duration(conf.RetryInterval) * time.Second)
	assert.NotNil(t, g2)
	assert.Equal(t, gid, g2.Gid)

	s.ChangeGlobalStatus(g, "succeed", []string{}, true)
	g2 = s.LockOneGlobalTrans(2 * time.Duration(conf.RetryInterval) * time.Second)
	assert.Nil(t, g2)
}

func TestStoreResetCronTime(t *testing.T) {
	s := registry.GetStore()
	testStoreResetCronTime(t, dtmimp.GetFuncName(), func(timeout int64, limit int64) error {
		return s.ResetCronTime(time.Duration(timeout)*time.Second, limit)
	})
}

func testStoreResetCronTime(t *testing.T, funcName string, restCronHandler func(expire int64, limit int64) error) {
	s := registry.GetStore()
	var restTimeTimeout, lockExpireIn, limit, i int64
	restTimeTimeout = 100 //The time that will be ResetCronTime
	lockExpireIn = 2      //The time that will be LockOneGlobalTrans
	limit = 10            // rest limit

	// Will be reset
	for i = 0; i < limit; i++ {
		gid := funcName + fmt.Sprintf("%d", i)
		_, _ = initTransGlobalByNextCronTime(gid, time.Now().Add(time.Duration(restTimeTimeout+10)*time.Second))
	}

	// Will not be reset
	gid := funcName + fmt.Sprintf("%d", 10)
	_, _ = initTransGlobalByNextCronTime(gid, time.Now().Add(time.Duration(restTimeTimeout-10)*time.Second))

	// Not Fount
	g := s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
	assert.Nil(t, g)

	// Rest limit-1 count
	err := restCronHandler(restTimeTimeout, limit-1)
	assert.Nil(t, err)
	// Fount limit-1 count
	for i = 0; i < limit-1; i++ {
		g = s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
		assert.NotNil(t, g)
		s.ChangeGlobalStatus(g, "succeed", []string{}, true)
	}

	// Not Fount
	g = s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
	assert.Nil(t, g)

	// Rest 1 count
	err = restCronHandler(restTimeTimeout, limit)
	// Fount 1 count
	g = s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
	assert.NotNil(t, g)
	s.ChangeGlobalStatus(g, "succeed", []string{}, true)

	// Not Fount
	g = s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
	assert.Nil(t, g)

	// Increase the restTimeTimeout, Rest 1 count
	err = restCronHandler(restTimeTimeout-12, limit)
	assert.Nil(t, err)
	// Fount 1 count
	g = s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
	assert.NotNil(t, g)
	s.ChangeGlobalStatus(g, "succeed", []string{}, true)

	// Not Fount
	g = s.LockOneGlobalTrans(time.Duration(lockExpireIn) * time.Second)
	assert.Nil(t, g)

}

func TestUpdateBranches(t *testing.T) {
	if !conf.Store.IsDB() {
		_, err := registry.GetStore().UpdateBranches(nil, nil)
		assert.Nil(t, err)
	}
}
