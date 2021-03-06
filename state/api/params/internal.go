// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package params

import (
	"launchpad.net/juju-core/agent/tools"
)

// Entity identifies a single entity.
type Entity struct {
	Tag string
}

// Entities identifies multiple entities.
type Entities struct {
	Entities []Entity
}

// StringsResult holds the result of an API call that returns a slice
// of strings.
type StringsResult struct {
	Result []string
}

// BytesResult holds the result of an API call that returns a slice
// of bytes.
type BytesResult struct {
	Result []byte
}

// LifeResult holds the life status of a single entity, or an error
// indicating why it is not available.
type LifeResult struct {
	Life  Life
	Error *Error
}

// LifeResults holds the life or error status of multiple entities.
type LifeResults struct {
	Results []LifeResult
}

// MachineSetStatus holds a machine tag, status and extra info.
type MachineSetStatus struct {
	Tag    string
	Status Status
	Info   string
}

// MachinesSetStatus holds the parameters for making a Machiner.SetStatus call.
type MachinesSetStatus struct {
	Machines []MachineSetStatus
}

// MachineAgentGetMachinesResults holds the results of a
// machineagent.API.GetMachines call.
type MachineAgentGetMachinesResults struct {
	Machines []MachineAgentGetMachinesResult
}

// MachineAgentGetMachinesResult holds the results of a
// machineagent.API.GetMachines call for a single machine.
type MachineAgentGetMachinesResult struct {
	Life  Life
	Jobs  []MachineJob
	Error *Error
}

// AgentToolsResult holds the tools and possibly error for a given Agent request
type AgentToolsResult struct {
	Tools *tools.Tools
	Error *Error
}

// AgentToolsResults is a list of tools for various requested agents.
type AgentToolsResults struct {
	Results []AgentToolsResult
}

// SetAgent specifies tools to be set for an agent with the
// given tag.
type SetAgentTools struct {
	Tag   string
	Tools *tools.Tools
}

// SetAgentsTools specifies what tools are being run for
// multiple agents.
type SetAgentsTools struct {
	AgentTools []SetAgentTools
}

// PasswordChanges holds the parameters for making a SetPasswords call.
type PasswordChanges struct {
	Changes []PasswordChange
}

// PasswordChange specifies a password change for the entity
// with the given tag.
type PasswordChange struct {
	Tag      string
	Password string
}

// NotifyWatchResult holds a NotifyWatcher id and an error (if any).
type NotifyWatchResult struct {
	NotifyWatcherId string
	Error           *Error
}

// NotifyWatchResults holds the results for any API call which ends up
// returning a list of NotifyWatchers
type NotifyWatchResults struct {
	Results []NotifyWatchResult
}

// StringsWatchResult holds a StringsWatcher id, changes and an error
// (if any).
type StringsWatchResult struct {
	StringsWatcherId string
	Changes          []string
	Error            *Error
}

// StringsWatchResults holds the results for any API call which ends up
// returning a list of StringsWatchers.
type StringsWatchResults struct {
	Results []StringsWatchResult
}
