package platform

import (
	ps "github.com/mitchellh/go-ps"
	"strings"
)

// ProcessIDsByName enumerates running processes and groups their PIDs by
// lowercased executable name, skipping pseudo-processes like the Windows
// "[System Process]" (pid 0)
func ProcessIDsByName() (map[string][]int, error) {
	processes, err := ps.Processes()
	if err != nil {
		return nil, err
	}

	pidsByName := make(map[string][]int, len(processes))
	for _, process := range processes {
		name := strings.ToLower(process.Executable())
		if name == "" || strings.HasPrefix(name, "[") {
			continue
		}

		pidsByName[name] = append(pidsByName[name], process.Pid())
	}

	return pidsByName, nil
}
