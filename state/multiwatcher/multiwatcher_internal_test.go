package multiwatcher

import (
	"container/list"
	"errors"
	"fmt"
	"labix.org/v2/mgo"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/state/api/params"
	"launchpad.net/juju-core/state/watcher"
	"launchpad.net/juju-core/testing"
	"sync"
	stdtesting "testing"
	"time"
)

func Test(t *stdtesting.T) {
	TestingT(t)
}

type storeSuite struct {
	testing.LoggingSuite
}

var _ = Suite(&storeSuite{})

var StoreChangeMethodTests = []struct {
	about          string
	change         func(all *Store)
	expectRevno    int64
	expectContents []entityEntry
}{{
	about:  "empty at first",
	change: func(*Store) {},
}, {
	about: "add single entry",
	change: func(all *Store) {
		storeAdd(all, &MachineInfo{
			Id:         "0",
			InstanceId: "i-0",
		})
	},
	expectRevno: 1,
	expectContents: []entityEntry{{
		creationRevno: 1,
		revno:         1,
		info: &MachineInfo{
			Id:         "0",
			InstanceId: "i-0",
		},
	}},
}, {
	about: "add two entries",
	change: func(all *Store) {
		storeAdd(all, &MachineInfo{
			Id:         "0",
			InstanceId: "i-0",
		})
		storeAdd(all, &ServiceInfo{
			Name:    "wordpress",
			Exposed: true,
		})
	},
	expectRevno: 2,
	expectContents: []entityEntry{{
		creationRevno: 1,
		revno:         1,
		info: &MachineInfo{
			Id:         "0",
			InstanceId: "i-0",
		},
	}, {
		creationRevno: 2,
		revno:         2,
		info: &ServiceInfo{
			Name:    "wordpress",
			Exposed: true,
		},
	}},
}, {
	about: "update an entity that's not currently there",
	change: func(all *Store) {
		m := &MachineInfo{Id: "1"}
		all.Update(idForInfo(m), m)
	},
	expectRevno: 1,
	expectContents: []entityEntry{{
		creationRevno: 1,
		revno:         1,
		info:          &MachineInfo{Id: "1"},
	}},
}, {
	about: "mark removed on existing entry",
	change: func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "0"})
		storeAdd(all, &MachineInfo{Id: "1"})
		StoreIncRef(all, testInfoId{"machine", "0"})
		all.Update(testInfoId{"machine", "0"}, nil)
	},
	expectRevno: 3,
	expectContents: []entityEntry{{
		creationRevno: 2,
		revno:         2,
		info:          &MachineInfo{Id: "1"},
	}, {
		creationRevno: 1,
		revno:         3,
		refCount:      1,
		removed:       true,
		info:          &MachineInfo{Id: "0"},
	}},
}, {
	about: "mark removed on nonexistent entry",
	change: func(all *Store) {
		all.Update(testInfoId{"machine", "0"}, nil)
	},
}, {
	about: "mark removed on already marked entry",
	change: func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "0"})
		storeAdd(all, &MachineInfo{Id: "1"})
		StoreIncRef(all, testInfoId{"machine", "0"})
		all.Update(testInfoId{"machine", "0"}, nil)
		all.Update(testInfoId{"machine", "1"}, &MachineInfo{
			Id:         "1",
			InstanceId: "i-1",
		})
		all.Update(testInfoId{"machine", "0"}, nil)
	},
	expectRevno: 4,
	expectContents: []entityEntry{{
		creationRevno: 1,
		revno:         3,
		refCount:      1,
		removed:       true,
		info:          &MachineInfo{Id: "0"},
	}, {
		creationRevno: 2,
		revno:         4,
		info: &MachineInfo{
			Id:         "1",
			InstanceId: "i-1",
		},
	}},
}, {
	about: "mark removed on entry with zero ref count",
	change: func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "0"})
		all.Update(testInfoId{"machine", "0"}, nil)
	},
	expectRevno: 2,
}, {
	about: "delete entry",
	change: func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "0"})
		all.delete(testInfoId{"machine", "0"})
	},
	expectRevno: 1,
}, {
	about: "decref of non-removed entity",
	change: func(all *Store) {
		m := &MachineInfo{Id: "0"}
		id := idForInfo(m)
		storeAdd(all, m)
		StoreIncRef(all, id)
		entry := all.entities[id].Value.(*entityEntry)
		all.decRef(entry, id)
	},
	expectRevno: 1,
	expectContents: []entityEntry{{
		creationRevno: 1,
		revno:         1,
		refCount:      0,
		info:          &MachineInfo{Id: "0"},
	}},
}, {
	about: "decref of removed entity",
	change: func(all *Store) {
		m := &MachineInfo{Id: "0"}
		id := idForInfo(m)
		storeAdd(all, m)
		entry := all.entities[id].Value.(*entityEntry)
		entry.refCount++
		all.Update(id, nil)
		all.decRef(entry, id)
	},
	expectRevno: 2,
},
}

func (s *storeSuite) TestStoreChangeMethods(c *C) {
	for i, test := range StoreChangeMethodTests {
		all := NewStore()
		c.Logf("test %d. %s", i, test.about)
		test.change(all)
		assertStoreContents(c, all, test.expectRevno, test.expectContents)
	}
}

func (s *storeSuite) TestChangesSince(c *C) {
	a := NewStore()
	// Add three entries.
	var deltas []params.Delta
	for i := 0; i < 3; i++ {
		m := &MachineInfo{Id: fmt.Sprint(i)}
		storeAdd(a, m)
		deltas = append(deltas, params.Delta{Entity: m})
	}
	// Check that the deltas from each revno are as expected.
	for i := 0; i < 3; i++ {
		c.Logf("test %d", i)
		c.Assert(a.ChangesSince(int64(i)), DeepEquals, deltas[i:])
	}

	// Check boundary cases.
	c.Assert(a.ChangesSince(-1), DeepEquals, deltas)
	c.Assert(a.ChangesSince(99), HasLen, 0)

	// Update one machine and check we see the changes.
	rev := a.latestRevno
	m1 := &MachineInfo{
		Id:         "1",
		InstanceId: "foo",
	}
	a.Update(idForInfo(m1), m1)
	c.Assert(a.ChangesSince(rev), DeepEquals, []params.Delta{{Entity: m1}})

	// Make sure the machine isn't simply removed from
	// the list when it's marked as removed.
	StoreIncRef(a, testInfoId{"machine", "0"})

	// Remove another machine and check we see it's removed.
	m0 := &MachineInfo{Id: "0"}
	a.Update(idForInfo(m0), nil)

	// Check that something that never saw m0 does not get
	// informed of its removal (even those the removed entity
	// is still in the list.
	c.Assert(a.ChangesSince(0), DeepEquals, []params.Delta{{
		Entity: &MachineInfo{Id: "2"},
	}, {
		Entity: m1,
	}})

	c.Assert(a.ChangesSince(rev), DeepEquals, []params.Delta{{
		Entity: m1,
	}, {
		Removed: true,
		Entity:  m0,
	}})

	c.Assert(a.ChangesSince(rev+1), DeepEquals, []params.Delta{{
		Removed: true,
		Entity:  m0,
	}})

}

type storeManagerSuite struct {
	testing.LoggingSuite
}

var _ = Suite(&storeManagerSuite{})

func (*storeManagerSuite) TestHandle(c *C) {
	sm := newStoreManagerNoRun(newTestBacking(nil))

	// Add request from first watcher.
	w0 := &Watcher{all: sm}
	req0 := &request{
		w:     w0,
		reply: make(chan bool, 1),
	}
	sm.handle(req0)
	assertWaitingRequests(c, sm, map[*Watcher][]*request{
		w0: {req0},
	})

	// Add second request from first watcher.
	req1 := &request{
		w:     w0,
		reply: make(chan bool, 1),
	}
	sm.handle(req1)
	assertWaitingRequests(c, sm, map[*Watcher][]*request{
		w0: {req1, req0},
	})

	// Add request from second watcher.
	w1 := &Watcher{all: sm}
	req2 := &request{
		w:     w1,
		reply: make(chan bool, 1),
	}
	sm.handle(req2)
	assertWaitingRequests(c, sm, map[*Watcher][]*request{
		w0: {req1, req0},
		w1: {req2},
	})

	// Stop first watcher.
	sm.handle(&request{
		w: w0,
	})
	assertWaitingRequests(c, sm, map[*Watcher][]*request{
		w1: {req2},
	})
	assertReplied(c, false, req0)
	assertReplied(c, false, req1)

	// Stop second watcher.
	sm.handle(&request{
		w: w1,
	})
	assertWaitingRequests(c, sm, nil)
	assertReplied(c, false, req2)
}

func (s *storeManagerSuite) TestHandleStopNoDecRefIfMoreRecentlyCreated(c *C) {
	// If the Watcher hasn't seen the item, then we shouldn't
	// decrement its ref count when it is stopped.
	sm := newStoreManagerNoRun(newTestBacking(nil))
	storeAdd(sm.all, &MachineInfo{Id: "0"})
	StoreIncRef(sm.all, testInfoId{"machine", "0"})
	w := &Watcher{all: sm}

	// Stop the watcher.
	sm.handle(&request{w: w})
	assertStoreContents(c, sm.all, 1, []entityEntry{{
		creationRevno: 1,
		revno:         1,
		refCount:      1,
		info: &MachineInfo{
			Id: "0",
		},
	}})
}

func (s *storeManagerSuite) TestHandleStopNoDecRefIfAlreadySeenRemoved(c *C) {
	// If the Watcher has already seen the item removed, then
	// we shouldn't decrement its ref count when it is stopped.
	sm := newStoreManagerNoRun(newTestBacking(nil))
	storeAdd(sm.all, &MachineInfo{Id: "0"})
	StoreIncRef(sm.all, testInfoId{"machine", "0"})
	sm.all.Update(testInfoId{"machine", "0"}, nil)
	w := &Watcher{all: sm}
	// Stop the watcher.
	sm.handle(&request{w: w})
	assertStoreContents(c, sm.all, 2, []entityEntry{{
		creationRevno: 1,
		revno:         2,
		refCount:      1,
		removed:       true,
		info: &MachineInfo{
			Id: "0",
		},
	}})
}

func (s *storeManagerSuite) TestHandleStopDecRefIfAlreadySeenAndNotRemoved(c *C) {
	// If the Watcher has already seen the item removed, then
	// we should decrement its ref count when it is stopped.
	sm := newStoreManagerNoRun(newTestBacking(nil))
	storeAdd(sm.all, &MachineInfo{Id: "0"})
	StoreIncRef(sm.all, testInfoId{"machine", "0"})
	w := &Watcher{all: sm}
	w.revno = sm.all.latestRevno
	// Stop the watcher.
	sm.handle(&request{w: w})
	assertStoreContents(c, sm.all, 1, []entityEntry{{
		creationRevno: 1,
		revno:         1,
		info: &MachineInfo{
			Id: "0",
		},
	}})
}

func (s *storeManagerSuite) TestHandleStopNoDecRefIfNotSeen(c *C) {
	// If the Watcher hasn't seen the item at all, it should
	// leave the ref count untouched.
	sm := newStoreManagerNoRun(newTestBacking(nil))
	storeAdd(sm.all, &MachineInfo{Id: "0"})
	StoreIncRef(sm.all, testInfoId{"machine", "0"})
	w := &Watcher{all: sm}
	// Stop the watcher.
	sm.handle(&request{w: w})
	assertStoreContents(c, sm.all, 1, []entityEntry{{
		creationRevno: 1,
		revno:         1,
		refCount:      1,
		info: &MachineInfo{
			Id: "0",
		},
	}})
}

var respondTestChanges = [...]func(all *Store){
	func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "0"})
	},
	func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "1"})
	},
	func(all *Store) {
		storeAdd(all, &MachineInfo{Id: "2"})
	},
	func(all *Store) {
		all.Update(testInfoId{"machine", "0"}, nil)
	},
	func(all *Store) {
		all.Update(testInfoId{"machine", "1"}, &MachineInfo{
			Id:         "1",
			InstanceId: "i-1",
		})
	},
	func(all *Store) {
		all.Update(testInfoId{"machine", "1"}, nil)
	},
}

var (
	respondTestFinalState = []entityEntry{{
		creationRevno: 3,
		revno:         3,
		info: &MachineInfo{
			Id: "2",
		},
	}}
	respondTestFinalRevno = int64(len(respondTestChanges))
)

func (s *storeManagerSuite) TestRespondResults(c *C) {
	// We test the response results for a pair of watchers by
	// interleaving notional Next requests in all possible
	// combinations after each change in respondTestChanges and
	// checking that the view of the world as seen by the watchers
	// matches the actual current state.

	// We decide whether if we make a request for a given
	// watcher by inspecting a number n - bit i of n determines whether
	// a request will be responded to after running respondTestChanges[i].

	numCombinations := 1 << uint(len(respondTestChanges))
	const wcount = 2
	ns := make([]int, wcount)
	for ns[0] = 0; ns[0] < numCombinations; ns[0]++ {
		for ns[1] = 0; ns[1] < numCombinations; ns[1]++ {
			sm := newStoreManagerNoRun(&storeManagerTestBacking{})
			c.Logf("test %0*b", len(respondTestChanges), ns)
			var (
				ws      []*Watcher
				wstates []watcherState
				reqs    []*request
			)
			for i := 0; i < wcount; i++ {
				ws = append(ws, &Watcher{})
				wstates = append(wstates, make(watcherState))
				reqs = append(reqs, nil)
			}
			// Make each change in turn, and make a request for each
			// watcher if n and respond
			for i, change := range respondTestChanges {
				c.Logf("change %d", i)
				change(sm.all)
				needRespond := false
				for wi, n := range ns {
					if n&(1<<uint(i)) != 0 {
						needRespond = true
						if reqs[wi] == nil {
							reqs[wi] = &request{
								w:     ws[wi],
								reply: make(chan bool, 1),
							}
							sm.handle(reqs[wi])
						}
					}
				}
				if !needRespond {
					continue
				}
				// Check that the expected requests are pending.
				expectWaiting := make(map[*Watcher][]*request)
				for wi, w := range ws {
					if reqs[wi] != nil {
						expectWaiting[w] = []*request{reqs[wi]}
					}
				}
				assertWaitingRequests(c, sm, expectWaiting)
				// Actually respond; then check that each watcher with
				// an outstanding request now has an up to date view
				// of the world.
				sm.respond()
				for wi, req := range reqs {
					if req == nil {
						continue
					}
					select {
					case ok := <-req.reply:
						c.Assert(ok, Equals, true)
						c.Assert(len(req.changes) > 0, Equals, true)
						wstates[wi].update(req.changes)
						reqs[wi] = nil
					default:
					}
					c.Logf("check %d", wi)
					wstates[wi].check(c, sm.all)
				}
			}
			// Stop the watcher and check that all ref counts end up at zero
			// and removed objects are deleted.
			for wi, w := range ws {
				sm.handle(&request{w: w})
				if reqs[wi] != nil {
					assertReplied(c, false, reqs[wi])
				}
			}
			assertStoreContents(c, sm.all, respondTestFinalRevno, respondTestFinalState)
		}
	}
}

func (*storeManagerSuite) TestRespondMultiple(c *C) {
	sm := newStoreManagerNoRun(newTestBacking(nil))
	storeAdd(sm.all, &MachineInfo{Id: "0"})

	// Add one request and respond.
	// It should see the above change.
	w0 := &Watcher{all: sm}
	req0 := &request{
		w:     w0,
		reply: make(chan bool, 1),
	}
	sm.handle(req0)
	sm.respond()
	assertReplied(c, true, req0)
	c.Assert(req0.changes, DeepEquals, []params.Delta{{Entity: &MachineInfo{Id: "0"}}})
	assertWaitingRequests(c, sm, nil)

	// Add another request from the same watcher and respond.
	// It should have no reply because nothing has changed.
	req0 = &request{
		w:     w0,
		reply: make(chan bool, 1),
	}
	sm.handle(req0)
	sm.respond()
	assertNotReplied(c, req0)

	// Add two requests from another watcher and respond.
	// The request from the first watcher should still not
	// be replied to, but the later of the two requests from
	// the second watcher should get a reply.
	w1 := &Watcher{all: sm}
	req1 := &request{
		w:     w1,
		reply: make(chan bool, 1),
	}
	sm.handle(req1)
	req2 := &request{
		w:     w1,
		reply: make(chan bool, 1),
	}
	sm.handle(req2)
	assertWaitingRequests(c, sm, map[*Watcher][]*request{
		w0: {req0},
		w1: {req2, req1},
	})
	sm.respond()
	assertNotReplied(c, req0)
	assertNotReplied(c, req1)
	assertReplied(c, true, req2)
	c.Assert(req2.changes, DeepEquals, []params.Delta{{Entity: &MachineInfo{Id: "0"}}})
	assertWaitingRequests(c, sm, map[*Watcher][]*request{
		w0: {req0},
		w1: {req1},
	})

	// Check that nothing more gets responded to if we call respond again.
	sm.respond()
	assertNotReplied(c, req0)
	assertNotReplied(c, req1)

	// Now make a change and check that both waiting requests
	// get serviced.
	storeAdd(sm.all, &MachineInfo{Id: "1"})
	sm.respond()
	assertReplied(c, true, req0)
	assertReplied(c, true, req1)
	assertWaitingRequests(c, sm, nil)

	deltas := []params.Delta{{Entity: &MachineInfo{Id: "1"}}}
	c.Assert(req0.changes, DeepEquals, deltas)
	c.Assert(req1.changes, DeepEquals, deltas)
}

func (*storeManagerSuite) TestRunStop(c *C) {
	sm := NewStoreManager(newTestBacking(nil))
	w := &Watcher{all: sm}
	err := sm.Stop()
	c.Assert(err, IsNil)
	d, err := w.Next()
	c.Assert(err, ErrorMatches, "state watcher was stopped")
	c.Assert(d, HasLen, 0)
}

func (*storeManagerSuite) TestRun(c *C) {
	b := newTestBacking([]testInfo{
		&MachineInfo{Id: "0"},
		&ServiceInfo{Name: "logging"},
		&ServiceInfo{Name: "wordpress"},
	})
	sm := NewStoreManager(b)
	defer func() {
		c.Check(sm.Stop(), IsNil)
	}()
	w := &Watcher{all: sm}
	checkNext(c, w, []params.Delta{
		{Entity: &MachineInfo{Id: "0"}},
		{Entity: &ServiceInfo{Name: "logging"}},
		{Entity: &ServiceInfo{Name: "wordpress"}},
	}, "")
	b.updateEntity(&MachineInfo{Id: "0", InstanceId: "i-0"})
	checkNext(c, w, []params.Delta{
		{Entity: &MachineInfo{Id: "0", InstanceId: "i-0"}},
	}, "")
	b.deleteEntity(testInfoId{"machine", "0"})
	checkNext(c, w, []params.Delta{
		{Removed: true, Entity: &MachineInfo{Id: "0"}},
	}, "")
}

func (*storeManagerSuite) TestWatcherStop(c *C) {
	sm := NewStoreManager(newTestBacking(nil))
	defer func() {
		c.Check(sm.Stop(), IsNil)
	}()
	w := &Watcher{all: sm}
	done := make(chan struct{})
	go func() {
		checkNext(c, w, nil, ErrWatcherStopped.Error())
		done <- struct{}{}
	}()
	err := w.Stop()
	c.Assert(err, IsNil)
	<-done
}

func (*storeManagerSuite) TestWatcherStopBecauseStoreManagerError(c *C) {
	b := newTestBacking([]testInfo{&MachineInfo{Id: "0"}})
	sm := NewStoreManager(b)
	defer func() {
		c.Check(sm.Stop(), ErrorMatches, "some error")
	}()
	w := &Watcher{all: sm}
	// Receive one delta to make sure that the storeManager
	// has seen the initial state.
	checkNext(c, w, []params.Delta{{Entity: &MachineInfo{Id: "0"}}}, "")
	c.Logf("setting fetch error")
	b.setFetchError(errors.New("some error"))
	c.Logf("updating entity")
	b.updateEntity(&MachineInfo{Id: "1"})
	checkNext(c, w, nil, "some error")
}

func idForInfo(info testInfo) InfoId {
	return testInfoId{
		kind: info.EntityKind(),
		id:   info.EntityId(),
	}
}

func storeAdd(a *Store, info testInfo) {
	a.add(idForInfo(info), info)
}

func StoreIncRef(a *Store, id InfoId) {
	entry := a.entities[id].Value.(*entityEntry)
	entry.refCount++
}

func assertStoreContents(c *C, a *Store, latestRevno int64, entries []entityEntry) {
	var gotEntries []entityEntry
	var gotElems []*list.Element
	c.Check(a.list.Len(), Equals, len(entries))
	for e := a.list.Back(); e != nil; e = e.Prev() {
		gotEntries = append(gotEntries, *e.Value.(*entityEntry))
		gotElems = append(gotElems, e)
	}
	c.Assert(gotEntries, DeepEquals, entries)
	for i, ent := range entries {
		c.Assert(a.entities[idForInfo(ent.info.(testInfo))], Equals, gotElems[i])
	}
	c.Assert(a.entities, HasLen, len(entries))
	c.Assert(a.latestRevno, Equals, latestRevno)
}

var errTimeout = errors.New("no change received in sufficient time")

func getNext(c *C, w *Watcher, timeout time.Duration) ([]params.Delta, error) {
	var deltas []params.Delta
	var err error
	ch := make(chan struct{}, 1)
	go func() {
		deltas, err = w.Next()
		ch <- struct{}{}
	}()
	select {
	case <-ch:
		return deltas, err
	case <-time.After(1 * time.Second):
	}
	return nil, errTimeout
}

func checkNext(c *C, w *Watcher, deltas []params.Delta, expectErr string) {
	d, err := getNext(c, w, 1*time.Second)
	if expectErr != "" {
		c.Check(err, ErrorMatches, expectErr)
		return
	}
	checkDeltasEqual(c, d, deltas)
}

// deltas are returns in arbitrary order, so we compare
// them as sets.
func checkDeltasEqual(c *C, d0, d1 []params.Delta) {
	c.Check(deltaMap(d0), DeepEquals, deltaMap(d1))
}

func deltaMap(deltas []params.Delta) map[InfoId]params.EntityInfo {
	m := make(map[InfoId]params.EntityInfo)
	for _, d := range deltas {
		id := idForInfo(d.Entity.(testInfo))
		if _, ok := m[id]; ok {
			panic(fmt.Errorf("%v mentioned twice in delta set", id))
		}
		if d.Removed {
			m[id] = nil
		} else {
			m[id] = d.Entity
		}
	}
	return m
}

// watcherState represents a Watcher client's
// current view of the state. It holds the last delta that a given
// state watcher has seen for each entity.
type watcherState map[InfoId]params.Delta

func (s watcherState) update(changes []params.Delta) {
	for _, d := range changes {
		id := idForInfo(d.Entity.(testInfo))
		if d.Removed {
			if _, ok := s[id]; !ok {
				panic(fmt.Errorf("entity id %v removed when it wasn't there", id))
			}
			delete(s, id)
		} else {
			s[id] = d
		}
	}
}

// check checks that the watcher state matches that
// held in current.
func (s watcherState) check(c *C, current *Store) {
	currentEntities := make(watcherState)
	for id, elem := range current.entities {
		entry := elem.Value.(*entityEntry)
		if !entry.removed {
			currentEntities[id] = params.Delta{Entity: entry.info}
		}
	}
	c.Assert(s, DeepEquals, currentEntities)
}

func assertNotReplied(c *C, req *request) {
	select {
	case v := <-req.reply:
		c.Fatalf("request was unexpectedly replied to (got %v)", v)
	default:
	}
}

func assertReplied(c *C, val bool, req *request) {
	select {
	case v := <-req.reply:
		c.Assert(v, Equals, val)
	default:
		c.Fatalf("request was not replied to")
	}
}

func assertWaitingRequests(c *C, sm *StoreManager, waiting map[*Watcher][]*request) {
	c.Assert(sm.waiting, HasLen, len(waiting))
	for w, reqs := range waiting {
		i := 0
		for req := sm.waiting[w]; ; req = req.next {
			if i >= len(reqs) {
				c.Assert(req, IsNil)
				break
			}
			c.Assert(req, Equals, reqs[i])
			assertNotReplied(c, req)
			i++
		}
	}
}

type storeManagerTestBacking struct {
	mu       sync.Mutex
	fetchErr error
	entities map[InfoId]testInfo
	watchc   chan<- watcher.Change
	txnRevno int64
}

func newTestBacking(initial []testInfo) *storeManagerTestBacking {
	b := &storeManagerTestBacking{
		entities: make(map[InfoId]testInfo),
	}
	for _, info := range initial {
		b.entities[idForInfo(info)] = info
	}
	return b
}

type testInfoId struct {
	kind string
	id   string
}

type testInfo interface {
	EntityKind() string
	EntityId() string
}

func (b *storeManagerTestBacking) Changed(all *Store, change watcher.Change) error {
	id := testInfoId{
		kind: change.C,
		id:   change.Id.(string),
	}
	info, err := b.fetch(id)
	if err == mgo.ErrNotFound {
		all.Update(id, nil)
		return nil
	}
	if err != nil {
		return err
	}
	all.Update(id, info)
	return nil
}

func (b *storeManagerTestBacking) fetch(id InfoId) (testInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fetchErr != nil {
		return nil, b.fetchErr
	}
	if info, ok := b.entities[id]; ok {
		return info, nil
	}
	return nil, mgo.ErrNotFound
}

func (b *storeManagerTestBacking) IdForInfo(info params.EntityInfo) InfoId {
	return idForInfo(info.(testInfo))
}

func (b *storeManagerTestBacking) Watch(c chan<- watcher.Change) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.watchc != nil {
		panic("test backing can only watch once")
	}
	b.watchc = c
}

func (b *storeManagerTestBacking) Unwatch(c chan<- watcher.Change) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c != b.watchc {
		panic("unwatching wrong channel")
	}
	b.watchc = nil
}

func (b *storeManagerTestBacking) GetAll(all *Store) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, info := range b.entities {
		all.Update(id, info)
	}
	return nil
}

func (b *storeManagerTestBacking) updateEntity(info testInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.IdForInfo(info).(testInfoId)
	b.entities[id] = info
	b.txnRevno++
	if b.watchc != nil {
		b.watchc <- watcher.Change{
			C:     id.kind,
			Id:    id.id,
			Revno: b.txnRevno, // This is actually ignored, but fill it in anyway.
		}
	}
}

func (b *storeManagerTestBacking) setFetchError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fetchErr = err
}

func (b *storeManagerTestBacking) deleteEntity(id0 InfoId) {
	id := id0.(testInfoId)
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entities, id)
	b.txnRevno++
	if b.watchc != nil {
		b.watchc <- watcher.Change{
			C:     id.kind,
			Id:    id.id,
			Revno: -1,
		}
	}
}

type MachineInfo struct {
	Id         string
	InstanceId string
}

func (i *MachineInfo) EntityId() string   { return i.Id }
func (i *MachineInfo) EntityKind() string { return "machine" }

type ServiceInfo struct {
	Name    string
	Exposed bool
}

func (i *ServiceInfo) EntityId() string   { return i.Name }
func (i *ServiceInfo) EntityKind() string { return "service" }