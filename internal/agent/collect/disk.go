package collect

import (
	"errors"
	"strconv"
	"strings"
)

// maxMounts bounds the disk series per host.
const maxMounts = 16

// realFilesystems are the block-backed filesystems worth monitoring; pseudo and overlay
// filesystems are deliberately excluded.
var realFilesystems = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true,
}

// FSStat is the result of statfs for one mount, in blocks.
type FSStat struct {
	BlockSize uint64
	Blocks    uint64 // total
	Free      uint64 // free including root-reserved
	Avail     uint64 // available to unprivileged users
}

// Disk reports usage per real filesystem, one series per underlying device.
type Disk struct {
	Proc   Proc
	Statfs func(path string) (FSStat, error)
}

// NewDisk returns a Disk collector reading the real system.
func NewDisk() *Disk { return &Disk{Proc: DefaultProc, Statfs: statfs} }

func (*Disk) Name() string { return "disk" }

type mount struct{ device, point string }

func (d *Disk) Collect() ([]Sample, error) {
	raw, err := d.Proc.read("mounts")
	if err != nil {
		return nil, err
	}
	var out []Sample
	var lastErr error
	for _, m := range parseMounts(raw) {
		st, err := d.Statfs(m.point)
		if err != nil {
			lastErr = err
			continue
		}
		if st.Blocks == 0 || st.BlockSize == 0 {
			continue
		}
		bs := float64(st.BlockSize)
		total := float64(st.Blocks) * bs
		used := float64(st.Blocks-min(st.Free, st.Blocks)) * bs
		// df semantics: percent of what an unprivileged user can use, so reserved blocks don't skew it.
		usable := float64(st.Blocks-min(st.Free, st.Blocks)+st.Avail) * bs
		pct := 0.0
		if usable > 0 {
			pct = clampPercent(100 * used / usable)
		}
		labels := map[string]string{"mount": m.point, "device": m.device}
		out = append(out,
			Sample{Name: "disk.total_bytes", Value: total, Labels: labels},
			Sample{Name: "disk.used_bytes", Value: used, Labels: labels},
			Sample{Name: "disk.used_percent", Value: pct, Labels: labels})
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// parseMounts returns real-filesystem mounts, one per device (first mount wins), at most maxMounts.
func parseMounts(raw string) []mount {
	var mounts []mount
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !realFilesystems[fields[2]] || seen[fields[0]] {
			continue
		}
		point := unescapeMount(fields[1])
		if len(point) > 128 || len(fields[0]) > 128 {
			continue // would violate the label value limit
		}
		seen[fields[0]] = true
		mounts = append(mounts, mount{device: fields[0], point: point})
		if len(mounts) == maxMounts {
			break
		}
	}
	return mounts
}

// unescapeMount decodes the octal escapes (\040 for a space) used in /proc/mounts.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var errUnsupported = errors.New("collect: statfs is only supported on Linux")
