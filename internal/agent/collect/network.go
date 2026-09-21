package collect

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// maxInterfaces bounds the network series per host (container hosts have many veth devices).
const maxInterfaces = 32

// Network reports monotonically increasing rx/tx byte counters per interface, excluding loopback.
type Network struct{ Proc Proc }

func (Network) Name() string { return "network" }

func (n Network) Collect() ([]Sample, error) {
	raw, err := n.Proc.read("net/dev")
	if err != nil {
		return nil, err
	}
	type counters struct{ rx, tx uint64 }
	byName := map[string]counters{}
	for _, line := range strings.Split(raw, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		fields := strings.Fields(rest)
		if name == "" || name == "lo" || len(name) > 64 || len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		byName[name] = counters{rx, tx}
	}
	if len(byName) == 0 {
		return nil, errors.New("collect: no network interfaces found in /proc/net/dev")
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxInterfaces {
		names = names[:maxInterfaces]
	}
	out := make([]Sample, 0, 2*len(names))
	for _, name := range names {
		c, labels := byName[name], map[string]string{"interface": name}
		out = append(out,
			Sample{Name: "network.rx_bytes_total", Value: float64(c.rx), Labels: labels},
			Sample{Name: "network.tx_bytes_total", Value: float64(c.tx), Labels: labels})
	}
	return out, nil
}
