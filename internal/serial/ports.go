package serial

import "go.bug.st/serial/enumerator"

// PortInfo describes a serial port independently of the transport library.
type PortInfo struct {
	Name    string
	IsUSB   bool
	VID     string
	PID     string
	Product string
}

// ListPorts enumerates the serial ports available on this machine.
func ListPorts() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	result := make([]PortInfo, 0, len(ports))
	for _, port := range ports {
		result = append(result, PortInfo{
			Name: port.Name, IsUSB: port.IsUSB, VID: port.VID, PID: port.PID,
			Product: port.Product,
		})
	}
	return result, nil
}
