// launchpad.net/juju/go/state
//
// Copyright (c) 2011-2012 Canonical Ltd.
package state_test

import (
	"fmt"
	. "launchpad.net/gocheck"
	"launchpad.net/gozk/zookeeper"
	"launchpad.net/juju/go/charm"
	"launchpad.net/juju/go/state"
	"launchpad.net/juju/go/testing"
	"net/url"
	"path/filepath"
	"sort"
	stdtesting "testing"
	"time"
)

// TestPackage integrates the tests into gotest.
func TestPackage(t *stdtesting.T) {
	srv := testing.StartZkServer(t)
	defer srv.Destroy()
	var err error
	state.TestingZkAddr, err = srv.Addr()
	if err != nil {
		t.Fatalf("could not get zk server address")
	}
	TestingT(t)
}

// charmDir returns a directory containing the given test charm.
func charmDir(name string) string {
	return filepath.Join("..", "charm", "testrepo", "series", name)
}

// readCharm returns a test charm by its name.
func readCharm(c *C, name string) charm.Charm {
	ch, err := charm.ReadDir(charmDir(name))
	c.Assert(err, IsNil)
	return ch
}

// localCharmURL returns the local URL of a charm.
func localCharmURL(ch charm.Charm) *charm.URL {
	url := fmt.Sprintf("local:series/%s-%d", ch.Meta().Name, ch.Revision())
	return charm.MustParseURL(url)
}

// addDummyCharm adds the 'dummy' charm state to st.
func addDummyCharm(c *C, st *state.State) (*state.Charm, *charm.URL) {
	ch := readCharm(c, "dummy")
	curl := localCharmURL(ch)
	bundleURL, err := url.Parse("http://bundle.url")
	c.Assert(err, IsNil)
	dummy, err := st.AddCharm(ch, curl, bundleURL)
	c.Assert(err, IsNil)
	return dummy, curl
}

type StateSuite struct {
	zkServer   *zookeeper.Server
	zkTestRoot string
	zkTestPort int
	zkAddr     string
	zkConn     *zookeeper.Conn
	st         *state.State
}

var _ = Suite(&StateSuite{})

func (s *StateSuite) SetUpTest(c *C) {
	var err error
	s.st, err = state.Initialize(&state.Info{
		Addrs: []string{state.TestingZkAddr},
	})
	c.Assert(err, IsNil)
	s.zkConn = state.ZkConn(s.st)
}

func (s *StateSuite) TearDownTest(c *C) {
	testing.ZkRemoveTree(c, s.zkConn, "/")
	s.zkConn.Close()
}

func (s StateSuite) TestInitialize(c *C) {
	info := &state.Info{
		Addrs: []string{state.TestingZkAddr},
	}
	// Check that initialization of an already-initialized state succeeds.
	st, err := state.Initialize(info)
	c.Assert(err, IsNil)
	c.Assert(st, NotNil)
	st.Close()

	// Check that Open blocks until Initialize has succeeded.
	testing.ZkRemoveTree(c, s.zkConn, "/")

	errc := make(chan error)
	go func() {
		st, err := state.Open(info)
		errc <- err
		if st != nil {
			st.Close()
		}
	}()

	// wait a little while to verify that it's actually blocking
	time.Sleep(200 * time.Millisecond)
	select {
	case err := <-errc:
		c.Fatalf("state.Open did not block (returned error %v)", err)
	default:
	}

	st, err = state.Initialize(info)
	c.Assert(err, IsNil)
	c.Assert(st, NotNil)
	defer st.Close()

	select {
	case err := <-errc:
		c.Assert(err, IsNil)
	case <-time.After(1e9):
		c.Fatalf("state.Open blocked forever")
	}
}

func (s StateSuite) TestAddCharm(c *C) {
	// Check that adding charms works correctly.
	dummyCharm := readCharm(c, "dummy")
	curl := localCharmURL(dummyCharm)
	bundleURL, err := url.Parse("http://bundle.url")
	c.Assert(err, IsNil)
	dummy, err := s.st.AddCharm(dummyCharm, curl, bundleURL)
	c.Assert(err, IsNil)
	c.Assert(dummy.URL().String(), Equals, curl.String())
	children, _, err := s.zkConn.Children("/charms")
	c.Assert(err, IsNil)
	c.Assert(children, DeepEquals, []string{"local_3a_series_2f_dummy-1"})
}

func (s StateSuite) TestCharmAttributes(c *C) {
	// Check that the basic (invariant) fields of the charm
	// are correctly in place.
	_, curl := addDummyCharm(c, s.st)

	dummy, err := s.st.Charm(curl)
	c.Assert(err, IsNil)
	c.Assert(dummy.URL().String(), Equals, curl.String())
	c.Assert(dummy.Revision(), Equals, 1)
	bundleURL, err := url.Parse("http://bundle.url")
	c.Assert(err, IsNil)
	c.Assert(dummy.BundleURL(), DeepEquals, bundleURL)
	meta := dummy.Meta()
	c.Assert(meta.Name, Equals, "dummy")
	config := dummy.Config()
	c.Assert(config.Options["title"], Equals,
		charm.Option{
			Default:     "My Title",
			Description: "A descriptive title used for the service.",
			Type:        "string",
		},
	)
}

func (s StateSuite) TestNonExistentCharmPriorToInitialization(c *C) {
	// Check that getting a charm before any other charm has been added fails nicely.
	curl, err := charm.ParseURL("local:series/dummy-1")
	c.Assert(err, IsNil)
	_, err = s.st.Charm(curl)
	c.Assert(err, ErrorMatches, `charm not found: "local:series/dummy-1"`)
}

func (s StateSuite) TestGetNonExistentCharm(c *C) {
	// Check that getting a non-existent charm fails nicely.
	addDummyCharm(c, s.st)

	curl := charm.MustParseURL("local:anotherseries/dummy-1")
	_, err := s.st.Charm(curl)
	c.Assert(err, ErrorMatches, `charm not found: "local:anotherseries/dummy-1"`)
}

func (s StateSuite) TestAddMachine(c *C) {
	machine0, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	c.Assert(machine0.Id(), Equals, 0)
	machine1, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	c.Assert(machine1.Id(), Equals, 1)

	children, _, err := s.zkConn.Children("/machines")
	c.Assert(err, IsNil)
	sort.Strings(children)
	c.Assert(children, DeepEquals, []string{"machine-0000000000", "machine-0000000001"})
}

func (s StateSuite) TestRemoveMachine(c *C) {
	machine, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	_, err = s.st.AddMachine()
	c.Assert(err, IsNil)
	err = s.st.RemoveMachine(machine.Id())
	c.Assert(err, IsNil)

	children, _, err := s.zkConn.Children("/machines")
	c.Assert(err, IsNil)
	sort.Strings(children)
	c.Assert(children, DeepEquals, []string{"machine-0000000001"})

	// Removing a non-existing machine has to fail.
	err = s.st.RemoveMachine(machine.Id())
	c.Assert(err, ErrorMatches, "can't remove machine 0: machine not found")
}

func (s StateSuite) TestReadMachine(c *C) {
	machine, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	expectedId := machine.Id()
	machine, err = s.st.Machine(expectedId)
	c.Assert(err, IsNil)
	c.Assert(machine.Id(), Equals, expectedId)
}

func (s StateSuite) TestReadNonExistentMachine(c *C) {
	_, err := s.st.Machine(0)
	c.Assert(err, ErrorMatches, "machine 0 not found")

	_, err = s.st.AddMachine()
	c.Assert(err, IsNil)
	_, err = s.st.Machine(1)
	c.Assert(err, ErrorMatches, "machine 1 not found")
}

func (s StateSuite) TestAllMachines(c *C) {
	machines, err := s.st.AllMachines()
	c.Assert(err, IsNil)
	c.Assert(len(machines), Equals, 0)

	_, err = s.st.AddMachine()
	c.Assert(err, IsNil)
	machines, err = s.st.AllMachines()
	c.Assert(err, IsNil)
	c.Assert(len(machines), Equals, 1)

	_, err = s.st.AddMachine()
	c.Assert(err, IsNil)
	machines, err = s.st.AllMachines()
	c.Assert(err, IsNil)
	c.Assert(len(machines), Equals, 2)
}

func (s StateSuite) TestAddService(c *C) {
	dummy, curl := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	c.Assert(wordpress.Name(), Equals, "wordpress")
	mysql, err := s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	c.Assert(mysql.Name(), Equals, "mysql")

	// Check that retrieving the new created services works correctly.
	wordpress, err = s.st.Service("wordpress")
	c.Assert(err, IsNil)
	c.Assert(wordpress.Name(), Equals, "wordpress")
	url, err := wordpress.CharmURL()
	c.Assert(err, IsNil)
	c.Assert(url.String(), Equals, curl.String())
	mysql, err = s.st.Service("mysql")
	c.Assert(err, IsNil)
	c.Assert(mysql.Name(), Equals, "mysql")
	url, err = mysql.CharmURL()
	c.Assert(err, IsNil)
	c.Assert(url.String(), Equals, curl.String())
}

func (s StateSuite) TestRemoveService(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	service, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)

	err = s.st.RemoveService(service)
	c.Assert(err, IsNil)
	service, err = s.st.Service("wordpress")
	c.Assert(err, ErrorMatches, `service with name "wordpress" not found`)
}

func (s StateSuite) TestReadNonExistentService(c *C) {
	_, err := s.st.Service("pressword")
	c.Assert(err, ErrorMatches, `service with name "pressword" not found`)
}

func (s StateSuite) TestAllServices(c *C) {
	services, err := s.st.AllServices()
	c.Assert(err, IsNil)
	c.Assert(len(services), Equals, 0)

	// Check that after adding services the result is ok.
	dummy, _ := addDummyCharm(c, s.st)
	_, err = s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	services, err = s.st.AllServices()
	c.Assert(err, IsNil)
	c.Assert(len(services), Equals, 1)

	_, err = s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	services, err = s.st.AllServices()
	c.Assert(err, IsNil)
	c.Assert(len(services), Equals, 2)

	// Check the returned service, order is defined by sorted keys.
	c.Assert(services[0].Name(), Equals, "wordpress")
	c.Assert(services[1].Name(), Equals, "mysql")
}

func (s StateSuite) TestServiceCharm(c *C) {
	dummy, curl := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)

	// Check that getting and setting the service charm URL works correctly.
	testcurl, err := wordpress.CharmURL()
	c.Assert(err, IsNil)
	c.Assert(testcurl.String(), Equals, curl.String())
	testcurl, err = charm.ParseURL("local:myseries/mydummy-1")
	c.Assert(err, IsNil)
	err = wordpress.SetCharmURL(testcurl)
	c.Assert(err, IsNil)
	testcurl, err = wordpress.CharmURL()
	c.Assert(err, IsNil)
	c.Assert(testcurl.String(), Equals, "local:myseries/mydummy-1")
}

func (s StateSuite) TestServiceExposed(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)

	// Check that querying for the exposed flag works correctly.
	exposed, err := wordpress.IsExposed()
	c.Assert(err, IsNil)
	c.Assert(exposed, Equals, false)

	// Check that setting and clearing the exposed flag works correctly.
	err = wordpress.SetExposed()
	c.Assert(err, IsNil)
	exposed, err = wordpress.IsExposed()
	c.Assert(err, IsNil)
	c.Assert(exposed, Equals, true)
	err = wordpress.ClearExposed()
	c.Assert(err, IsNil)
	exposed, err = wordpress.IsExposed()
	c.Assert(err, IsNil)
	c.Assert(exposed, Equals, false)

	// Check that setting and clearing the exposed flag multiple doesn't fail.
	err = wordpress.SetExposed()
	c.Assert(err, IsNil)
	err = wordpress.SetExposed()
	c.Assert(err, IsNil)
	err = wordpress.ClearExposed()
	c.Assert(err, IsNil)
	err = wordpress.ClearExposed()
	c.Assert(err, IsNil)

	// Check that setting and clearing the exposed flag on removed services also doesn't fail.
	err = s.st.RemoveService(wordpress)
	c.Assert(err, IsNil)
	err = wordpress.ClearExposed()
	c.Assert(err, IsNil)
}

func (s StateSuite) TestAddUnit(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)

	// Check that adding units works.
	unitZero, err := wordpress.AddUnit()
	c.Assert(err, IsNil)
	c.Assert(unitZero.Name(), Equals, "wordpress/0")
	unitOne, err := wordpress.AddUnit()
	c.Assert(err, IsNil)
	c.Assert(unitOne.Name(), Equals, "wordpress/1")
}

func (s StateSuite) TestReadUnit(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	_, err = wordpress.AddUnit()
	c.Assert(err, IsNil)
	_, err = wordpress.AddUnit()
	c.Assert(err, IsNil)
	mysql, err := s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	_, err = mysql.AddUnit()
	c.Assert(err, IsNil)

	// Check that retrieving a unit works correctly.
	unit, err := wordpress.Unit("wordpress/0")
	c.Assert(err, IsNil)
	c.Assert(unit.Name(), Equals, "wordpress/0")

	// Check that retrieving a non-existent or an invalidly
	// named unit fail nicely.
	unit, err = wordpress.Unit("wordpress")
	c.Assert(err, ErrorMatches, `"wordpress" is not a valid unit name`)
	unit, err = wordpress.Unit("wordpress/0/0")
	c.Assert(err, ErrorMatches, `"wordpress/0/0" is not a valid unit name`)
	unit, err = wordpress.Unit("pressword/0")
	c.Assert(err, ErrorMatches, `can't find unit "pressword/0" on service "wordpress"`)
	unit, err = wordpress.Unit("mysql/0")
	c.Assert(err, ErrorMatches, `can't find unit "mysql/0" on service "wordpress"`)

	// Check that retrieving unit names works.
	unitNames, err := wordpress.UnitNames()
	c.Assert(err, IsNil)
	c.Assert(unitNames, DeepEquals, []string{"wordpress/0", "wordpress/1"})

	// Check that retrieving all units works.
	units, err := wordpress.AllUnits()
	c.Assert(err, IsNil)
	c.Assert(len(units), Equals, 2)
	c.Assert(units[0].Name(), Equals, "wordpress/0")
	c.Assert(units[1].Name(), Equals, "wordpress/1")
}

func (s StateSuite) TestReadUnitWithChangingState(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)

	// Check that reading a unit after removing the service
	// fails nicely.
	err = s.st.RemoveService(wordpress)
	c.Assert(err, IsNil)
	_, err = s.st.Unit("wordpress/0")
	c.Assert(err, ErrorMatches, `service with name "wordpress" not found`)
}

func (s StateSuite) TestRemoveUnit(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	_, err = wordpress.AddUnit()
	c.Assert(err, IsNil)
	_, err = wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Check that removing a unit works.
	unit, err := wordpress.Unit("wordpress/0")
	c.Assert(err, IsNil)
	err = wordpress.RemoveUnit(unit)
	c.Assert(err, IsNil)
	unitNames, err := wordpress.UnitNames()
	c.Assert(err, IsNil)
	c.Assert(unitNames, DeepEquals, []string{"wordpress/1"})

	// Check that removing a non-existent unit fails nicely.
	err = wordpress.RemoveUnit(unit)
	c.Assert(err, ErrorMatches, "environment state has changed")
}

func (s StateSuite) TestGetSetPublicAddress(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Check that retrieving and setting of a public address works.
	address, err := unit.PublicAddress()
	c.Assert(err, ErrorMatches, "unit has no public address")
	err = unit.SetPublicAddress("example.foobar.com")
	c.Assert(err, IsNil)
	address, err = unit.PublicAddress()
	c.Assert(err, IsNil)
	c.Assert(address, Equals, "example.foobar.com")
}

func (s StateSuite) TestGetSetPrivateAddress(c *C) {
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Check that retrieving and setting of a private address works.
	address, err := unit.PrivateAddress()
	c.Assert(err, ErrorMatches, "unit has no private address")
	err = unit.SetPrivateAddress("example.local")
	c.Assert(err, IsNil)
	address, err = unit.PrivateAddress()
	c.Assert(err, IsNil)
	c.Assert(address, Equals, "example.local")
}

func (s StateSuite) TestUnitCharm(c *C) {
	dummy, curl := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Check that getting and setting the unit charm URL works correctly.
	testcurl, err := unit.CharmURL()
	c.Assert(err, IsNil)
	c.Assert(testcurl.String(), Equals, curl.String())
	testcurl, err = charm.ParseURL("local:myseries/mydummy-1")
	c.Assert(err, IsNil)
	err = unit.SetCharmURL(testcurl)
	c.Assert(err, IsNil)
	testcurl, err = unit.CharmURL()
	c.Assert(err, IsNil)
	c.Assert(testcurl.String(), Equals, "local:myseries/mydummy-1")
}

func (s StateSuite) TestUnassignUnitFromMachineWithoutBeingAssigned(c *C) {
	// When unassigning a machine from a unit, it is possible that
	// the machine has not been previously assigned, or that it
	// was assigned but the state changed beneath us.  In either
	// case, the end state is the intended state, so we simply
	// move forward without any errors here, to avoid having to
	// handle the extra complexity of dealing with the concurrency
	// problems.
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	err = unit.UnassignFromMachine()
	c.Assert(err, IsNil)

	// Check that the unit has no machine assigned.
	wordpress, err = s.st.Service("wordpress")
	c.Assert(err, IsNil)
	units, err := wordpress.AllUnits()
	c.Assert(err, IsNil)
	unit = units[0]
	_, err = unit.AssignedMachineId()
	c.Assert(err, ErrorMatches, `unit not assigned to machine`)
}

func (s StateSuite) TestAssignUnitToMachineAgainFails(c *C) {
	// Check that assigning an already assigned unit to
	// a machine fails if it isn't precisely the same
	// machine. 
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)
	machineOne, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	machineTwo, err := s.st.AddMachine()
	c.Assert(err, IsNil)

	err = unit.AssignToMachine(machineOne)
	c.Assert(err, IsNil)

	// Assigning the unit to the same machine should return no error.
	err = unit.AssignToMachine(machineOne)
	c.Assert(err, IsNil)

	// Assigning the unit to a different machine should fail.
	err = unit.AssignToMachine(machineTwo)
	c.Assert(err, ErrorMatches, `unit "wordpress/0" already assigned to machine 0`)

	machineId, err := unit.AssignedMachineId()
	c.Assert(err, IsNil)
	c.Assert(machineId, Equals, 0)
}

func (s StateSuite) TestUnassignUnitFromMachineWithChangingState(c *C) {
	// Check that unassigning while the state changes fails nicely.
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Remove the unit for the tests.
	wordpress, err = s.st.Service("wordpress")
	c.Assert(err, IsNil)
	units, err := wordpress.AllUnits()
	c.Assert(err, IsNil)
	unit = units[0]
	err = wordpress.RemoveUnit(unit)
	c.Assert(err, IsNil)

	err = unit.UnassignFromMachine()
	c.Assert(err, ErrorMatches, "environment state has changed")
	_, err = unit.AssignedMachineId()
	c.Assert(err, ErrorMatches, "environment state has changed")

	err = s.st.RemoveService(wordpress)
	c.Assert(err, IsNil)

	err = unit.UnassignFromMachine()
	c.Assert(err, ErrorMatches, "environment state has changed")
	_, err = unit.AssignedMachineId()
	c.Assert(err, ErrorMatches, "environment state has changed")
}

func (s StateSuite) TestAssignUnitToUnusedMachine(c *C) {
	// Create root machine that shouldn't be useds.
	_, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	// Check that a unit can be assigned to an unused machine.
	dummy, _ := addDummyCharm(c, s.st)
	mysqlService, err := s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	mysqlUnit, err := mysqlService.AddUnit()
	c.Assert(err, IsNil)
	mysqlMachine, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	err = mysqlUnit.AssignToMachine(mysqlMachine)
	c.Assert(err, IsNil)
	err = s.st.RemoveService(mysqlService)
	c.Assert(err, IsNil)

	wordpressService, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	wordpressUnit, err := wordpressService.AddUnit()
	c.Assert(err, IsNil)
	wordpressMachine, err := wordpressUnit.AssignToUnusedMachine()
	c.Assert(err, IsNil)

	c.Assert(wordpressMachine.Id(), Equals, mysqlMachine.Id())
}

func (s StateSuite) TestAssignUnitToUnusedMachineWithChangingService(c *C) {
	// Create root machine that shouldn't be useds.
	_, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	// Check for a 'state changed' error if a service is manipulated
	// during reuse.
	dummy, _ := addDummyCharm(c, s.st)
	mysqlService, err := s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	mysqlUnit, err := mysqlService.AddUnit()
	c.Assert(err, IsNil)
	mysqlMachine, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	err = mysqlUnit.AssignToMachine(mysqlMachine)
	c.Assert(err, IsNil)
	err = s.st.RemoveService(mysqlService)
	c.Assert(err, IsNil)

	wordpressService, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	wordpressUnit, err := wordpressService.AddUnit()
	c.Assert(err, IsNil)
	err = s.st.RemoveService(wordpressService)
	c.Assert(err, IsNil)

	_, err = wordpressUnit.AssignToUnusedMachine()
	c.Assert(err, ErrorMatches, "environment state has changed")
}

func (s StateSuite) TestAssignUnitToUnusedMachineWithChangingUnit(c *C) {
	// Create root machine that shouldn't be useds.
	_, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	// Check for a 'state changed' error if a unit is manipulated
	// during reuse.
	dummy, _ := addDummyCharm(c, s.st)
	mysqlService, err := s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	mysqlUnit, err := mysqlService.AddUnit()
	c.Assert(err, IsNil)
	mysqlMachine, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	err = mysqlUnit.AssignToMachine(mysqlMachine)
	c.Assert(err, IsNil)
	err = s.st.RemoveService(mysqlService)
	c.Assert(err, IsNil)

	wordpressService, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	wordpressUnit, err := wordpressService.AddUnit()
	c.Assert(err, IsNil)
	err = wordpressService.RemoveUnit(wordpressUnit)
	c.Assert(err, IsNil)

	_, err = wordpressUnit.AssignToUnusedMachine()
	c.Assert(err, ErrorMatches, "environment state has changed")
}

func (s StateSuite) TestAssignUnitToUnusedMachineOnlyZero(c *C) {
	// Create root machine that shouldn't be useds.
	_, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	// Check that the unit can't be assigned to machine zero.
	dummy, _ := addDummyCharm(c, s.st)
	wordpressService, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	wordpressUnit, err := wordpressService.AddUnit()
	c.Assert(err, IsNil)

	_, err = wordpressUnit.AssignToUnusedMachine()
	c.Assert(err, ErrorMatches, "no unused machine found")
}

func (s StateSuite) TestAssignUnitToUnusedMachineNoneAvailable(c *C) {
	// Create machine 0, that shouldn't be used.
	_, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	// Check that assigning without unused machine fails.   
	dummy, _ := addDummyCharm(c, s.st)
	mysqlService, err := s.st.AddService("mysql", dummy)
	c.Assert(err, IsNil)
	mysqlUnit, err := mysqlService.AddUnit()
	c.Assert(err, IsNil)
	mysqlMachine, err := s.st.AddMachine()
	c.Assert(err, IsNil)
	err = mysqlUnit.AssignToMachine(mysqlMachine)
	c.Assert(err, IsNil)

	wordpressService, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	wordpressUnit, err := wordpressService.AddUnit()
	c.Assert(err, IsNil)

	_, err = wordpressUnit.AssignToUnusedMachine()
	c.Assert(err, ErrorMatches, "no unused machine found")
}

func (s StateSuite) TestGetSetClearUnitUpgrate(c *C) {
	// Check that setting and clearing an upgrade flag on a unit works.
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Defaults to false.
	upgrade, err := unit.NeedsUpgrade()
	c.Assert(err, IsNil)
	c.Assert(upgrade, Equals, false)

	// Can be set.
	err = unit.SetNeedsUpgrade()
	c.Assert(err, IsNil)
	upgrade, err = unit.NeedsUpgrade()
	c.Assert(err, IsNil)
	c.Assert(upgrade, Equals, true)

	// Can be set multiple times.
	err = unit.SetNeedsUpgrade()
	c.Assert(err, IsNil)
	upgrade, err = unit.NeedsUpgrade()
	c.Assert(err, IsNil)
	c.Assert(upgrade, Equals, true)

	// Can be cleared.
	err = unit.ClearNeedsUpgrade()
	c.Assert(err, IsNil)
	upgrade, err = unit.NeedsUpgrade()
	c.Assert(err, IsNil)
	c.Assert(upgrade, Equals, false)

	// Can be cleared multiple times
	err = unit.ClearNeedsUpgrade()
	c.Assert(err, IsNil)
	upgrade, err = unit.NeedsUpgrade()
	c.Assert(err, IsNil)
	c.Assert(upgrade, Equals, false)
}

func (s StateSuite) TestGetSetClearResolved(c *C) {
	// Check that setting and clearing the resolved setting on a unit works.
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	setting, err := unit.Resolved()
	c.Assert(err, IsNil)
	c.Assert(setting, Equals, state.ResolvedNone)

	err = unit.SetResolved(state.ResolvedNoHooks)
	c.Assert(err, IsNil)
	err = unit.SetResolved(state.ResolvedNoHooks)
	c.Assert(err, ErrorMatches, `unit "wordpress/0" resolved flag already set`)
	retry, err := unit.Resolved()
	c.Assert(err, IsNil)
	c.Assert(retry, Equals, state.ResolvedNoHooks)

	err = unit.ClearResolved()
	c.Assert(err, IsNil)
	setting, err = unit.Resolved()
	c.Assert(err, IsNil)
	c.Assert(setting, Equals, state.ResolvedNone)
	err = unit.ClearResolved()
	c.Assert(err, IsNil)

	err = unit.SetResolved(state.ResolvedMode(999))
	c.Assert(err, ErrorMatches, `invalid error resolution mode: 999`)
}

func (s StateSuite) TestGetOpenPorts(c *C) {
	// Check that changes to the open ports of units work porperly.
	dummy, _ := addDummyCharm(c, s.st)
	wordpress, err := s.st.AddService("wordpress", dummy)
	c.Assert(err, IsNil)
	unit, err := wordpress.AddUnit()
	c.Assert(err, IsNil)

	// Verify no open ports before activity.
	open, err := unit.OpenPorts()
	c.Assert(err, IsNil)
	c.Assert(open, HasLen, 0)

	// Now open and close port.
	err = unit.OpenPort("tcp", 80)
	c.Assert(err, IsNil)
	open, err = unit.OpenPorts()
	c.Assert(err, IsNil)
	c.Assert(open, DeepEquals, []state.Port{
		{"tcp", 80},
	})

	err = unit.OpenPort("udp", 53)
	c.Assert(err, IsNil)
	open, err = unit.OpenPorts()
	c.Assert(err, IsNil)
	c.Assert(open, DeepEquals, []state.Port{
		{"tcp", 80},
		{"udp", 53},
	})

	err = unit.OpenPort("tcp", 53)
	c.Assert(err, IsNil)
	open, err = unit.OpenPorts()
	c.Assert(err, IsNil)
	c.Assert(open, DeepEquals, []state.Port{
		{"tcp", 80},
		{"udp", 53},
		{"tcp", 53},
	})

	err = unit.OpenPort("tcp", 443)
	c.Assert(err, IsNil)
	open, err = unit.OpenPorts()
	c.Assert(err, IsNil)
	c.Assert(open, DeepEquals, []state.Port{
		{"tcp", 80},
		{"udp", 53},
		{"tcp", 53},
		{"tcp", 443},
	})

	err = unit.ClosePort("tcp", 80)
	c.Assert(err, IsNil)
	open, err = unit.OpenPorts()
	c.Assert(err, IsNil)
	c.Assert(open, DeepEquals, []state.Port{
		{"udp", 53},
		{"tcp", 53},
		{"tcp", 443},
	})
}