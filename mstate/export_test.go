package mstate

type (
	CharmDoc   charmDoc
	MachineDoc machineDoc
	ServiceDoc serviceDoc
	UnitDoc    unitDoc
)

func (doc *MachineDoc) String() string {
	m := &Machine{id: doc.Id}
	return m.String()
}