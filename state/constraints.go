package state

import (
	"fmt"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/txn"
	"math"
	"strconv"
	"strings"
)

// Constraints describes a user's requirements of the hardware on which units
// of a service will run. Constraints are used to choose an existing machine
// onto which a unit will be deployed, or to provision a new machine if no
// existing one satisfies the requirements.
type Constraints struct {

	// Arch, if not nil or empty, indicates that a machine must run the named
	// architecture.
	Arch *string `json:"arch,omitempty" yaml:"arch,omitempty"`

	// CpuCores, if not nil, indicates that a machine must have at least that
	// number of effective cores available.
	CpuCores *uint64 `json:"cpu-cores,omitempty" yaml:"cpu-cores,omitempty"`

	// CpuPower, if not nil, indicates that a machine must have at least that
	// amount of CPU power available, where 100 CpuPower is considered to be
	// equivalent to 1 Amazon ECU (or, roughly, a single 2007-era Xeon).
	CpuPower *uint64 `json:"cpu-power,omitempty" yaml:"cpu-power,omitempty"`

	// Mem, if not nil, indicates that a machine must have at least that many
	// megabytes of RAM.
	Mem *uint64 `json:"mem,omitempty" yaml:"mem,omitempty"`
}

// String expresses a Constraints in the language in which it was specified.
func (c Constraints) String() string {
	var strs []string
	if c.Arch != nil {
		strs = append(strs, "arch="+*c.Arch)
	}
	if c.CpuCores != nil {
		strs = append(strs, "cpu-cores="+uintStr(*c.CpuCores))
	}
	if c.CpuPower != nil {
		strs = append(strs, "cpu-power="+uintStr(*c.CpuPower))
	}
	if c.Mem != nil {
		s := uintStr(*c.Mem)
		if s != "" {
			s += "M"
		}
		strs = append(strs, "mem="+s)
	}
	return strings.Join(strs, " ")
}

func uintStr(i uint64) string {
	if i == 0 {
		return ""
	}
	return fmt.Sprintf("%d", i)
}

// ParseConstraints constructs a Constraints from the supplied arguments,
// each of which must contain only spaces and name=value pairs. If any
// name is specified more than once, an error is returned.
func ParseConstraints(args ...string) (Constraints, error) {
	cons := Constraints{}
	for _, arg := range args {
		raws := strings.Split(strings.TrimSpace(arg), " ")
		for _, raw := range raws {
			if raw == "" {
				continue
			}
			if err := cons.setRaw(raw); err != nil {
				return Constraints{}, err
			}
		}
	}
	return cons, nil
}

// setRaw interprets a name=value string and sets the supplied value.
func (c *Constraints) setRaw(raw string) error {
	eq := strings.Index(raw, "=")
	if eq <= 0 {
		return fmt.Errorf("malformed constraint %q", raw)
	}
	name, str := raw[:eq], raw[eq+1:]
	var err error
	switch name {
	case "arch":
		err = c.setArch(str)
	case "cpu-cores":
		err = c.setCpuCores(str)
	case "cpu-power":
		err = c.setCpuPower(str)
	case "mem":
		err = c.setMem(str)
	default:
		return fmt.Errorf("unknown constraint %q", name)
	}
	if err != nil {
		return fmt.Errorf("bad %q constraint: %v", name, err)
	}
	return nil
}

func (c *Constraints) setArch(str string) error {
	if c.Arch != nil {
		return fmt.Errorf("already set")
	}
	switch str {
	case "":
	case "amd64", "i386", "arm":
	default:
		return fmt.Errorf("%q not recognized", str)
	}
	c.Arch = &str
	return nil
}

func (c *Constraints) setCpuCores(str string) (err error) {
	if c.CpuCores != nil {
		return fmt.Errorf("already set")
	}
	c.CpuCores, err = parseUint64(str)
	return
}

func (c *Constraints) setCpuPower(str string) (err error) {
	if c.CpuPower != nil {
		return fmt.Errorf("already set")
	}
	c.CpuPower, err = parseUint64(str)
	return
}

func (c *Constraints) setMem(str string) error {
	if c.Mem != nil {
		return fmt.Errorf("already set")
	}
	var value uint64
	if str != "" {
		mult := 1.0
		if m, ok := mbSuffixes[str[len(str)-1:]]; ok {
			str = str[:len(str)-1]
			mult = m
		}
		val, err := strconv.ParseFloat(str, 64)
		if err != nil || val < 0 {
			return fmt.Errorf("must be a non-negative float with optional M/G/T/P suffix")
		}
		val *= mult
		value = uint64(math.Ceil(val))
	}
	c.Mem = &value
	return nil
}

func parseUint64(str string) (*uint64, error) {
	var value uint64
	if str != "" {
		if val, err := strconv.Atoi(str); err != nil || val < 0 {
			return nil, fmt.Errorf("must be a non-negative integer")
		} else {
			value = uint64(val)
		}
	}
	return &value, nil
}

var mbSuffixes = map[string]float64{
	"M": 1,
	"G": 1024,
	"T": 1024 * 1024,
	"P": 1024 * 1024 * 1024,
}

// constraintsDoc is the mongodb representation of a Constraints.
type constraintsDoc struct {
	Arch     *string
	CpuCores *uint64
	CpuPower *uint64
	Mem      *uint64
}

func newConstraintsDoc(cons Constraints) constraintsDoc {
	return constraintsDoc{
		Arch:     cons.Arch,
		CpuCores: cons.CpuCores,
		CpuPower: cons.CpuPower,
		Mem:      cons.Mem,
	}
}

func createConstraintsOp(st *State, id string, cons Constraints) txn.Op {
	return txn.Op{
		C:      st.constraints.Name,
		Id:     id,
		Assert: txn.DocMissing,
		Insert: newConstraintsDoc(cons),
	}
}

func readConstraints(st *State, id string) (Constraints, error) {
	doc := constraintsDoc{}
	if err := st.constraints.FindId(id).One(&doc); err == mgo.ErrNotFound {
		return Constraints{}, NotFoundf("constraints")
	} else if err != nil {
		return Constraints{}, err
	}
	return Constraints{
		Arch:     doc.Arch,
		CpuCores: doc.CpuCores,
		CpuPower: doc.CpuPower,
		Mem:      doc.Mem,
	}, nil
}

func writeConstraints(st *State, id string, cons Constraints) error {
	ops := []txn.Op{{
		C:      st.constraints.Name,
		Id:     id,
		Assert: txn.DocExists,
		Update: D{{"$set", newConstraintsDoc(cons)}},
	}}
	if err := st.runner.Run(ops, "", nil); err != nil {
		return fmt.Errorf("cannot set constraints: %v", err)
	}
	return nil
}