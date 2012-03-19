package state

import (
	"errors"
	"fmt"
	"launchpad.net/gozk/zookeeper"
	"strings"
	"time"
)

// Info encapsulates information about cluster of
// servers holding juju state and can be used to make a
// connection to that cluster.
type Info struct {
	// Addrs gives the addresses of the Zookeeper
	// servers for the state. Each address should be in the form
	// address:port.
	Addrs []string
}

const zkTimeout = 15e9

// Open connects to the server described by the given
// info, waits for it to be initialized, and returns a new State
// representing the environment connected to.
func Open(info *Info) (*State, error) {
	st, err := open(info)
	if err != nil {
		return nil, err
	}
	err = st.waitForInitialization(3 * time.Minute)
	if err != nil {
		return nil, err
	}
	return st, err
}

func open(info *Info) (*State, error) {
	if len(info.Addrs) == 0 {
		return nil, fmt.Errorf("no zookeeper addresses")
	}
	zk, session, err := zookeeper.Dial(strings.Join(info.Addrs, ","), zkTimeout)
	if err != nil {
		return nil, err
	}
	if !(<-session).Ok() {
		return nil, errors.New("Could not connect to zookeeper")
	}

	// TODO decide what to do with session events - currently
	// we will panic if the session event channel fills up.
	return &State{zk}, nil
}

// Initialize sets up an initial empty state in ZooKeeper and returns
// it.  This needs to be performed only once for a given cluster of
// ZooKeeper servers.
func Initialize(info *Info) (*State, error) {
	st, err := open(info)
	if err != nil {
		return nil, err
	}
	err = st.initialize()
	if err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

func (s *State) initialize() error {
	already, err := s.initialized()
	if err != nil || already {
		return err
	}
	// Create new nodes.
	if _, err := s.zk.Create("/charms", "", 0, zkPermAll); err != nil {
		return err
	}
	if _, err := s.zk.Create("/services", "", 0, zkPermAll); err != nil {
		return err
	}
	if _, err := s.zk.Create("/machines", "", 0, zkPermAll); err != nil {
		return err
	}
	if _, err := s.zk.Create("/units", "", 0, zkPermAll); err != nil {
		return err
	}
	if _, err := s.zk.Create("/relations", "", 0, zkPermAll); err != nil {
		return err
	}
	// TODO Create node for bootstrap machine.

	// TODO Setup default global settings information.

	// Finally creation of /initialized as marker.
	if _, err := s.zk.Create("/initialized", "", 0, zkPermAll); err != nil {
		return err
	}
	return nil
}

func (s *State) initialized() (bool, error) {
	stat, err := s.zk.Exists("/initialized")
	if err != nil {
		return false, err
	}
	return stat != nil, nil
}

func (s *State) waitForInitialization(timeout time.Duration) error {
	stat, watch, err := s.zk.ExistsW("/initialized")
	if err != nil {
		return err
	}
	if stat != nil {
		return nil
	}
	select {
	case e := <-watch:
		if !e.Ok() {
			return fmt.Errorf("session error: %v", e)
		}
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for initialization")
	}
	return nil
}

func (st *State) Close() error {
	return st.zk.Close()
}